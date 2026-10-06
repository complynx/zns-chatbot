#!/usr/bin/env python3
"""Bounded Linux operator for an explicitly approved synthetic Compose project."""

import argparse
from contextlib import ExitStack, contextmanager
from decimal import Decimal
import hashlib
import json
import math
import os
from pathlib import Path
import re
import selectors
import signal
import stat
import subprocess
import sys
import threading
import time


class Failure(Exception):
    pass


DAEMON_SOURCE_PREFIX = "/run/desktop/mnt/host/c/Users/ddriz/Projects/zns-chatbot/"


REQUIRED_INVARIANTS = {
    "Config.User", "Config.Entrypoint", "Config.Cmd", "Config.Env", "Mounts",
    "HostConfig.NanoCpus", "HostConfig.Memory", "HostConfig.MemorySwap",
    "HostConfig.PidsLimit", "HostConfig.Privileged", "HostConfig.CapAdd",
    "HostConfig.CapDrop", "HostConfig.SecurityOpt", "HostConfig.ReadonlyRootfs",
    "HostConfig.RestartPolicy",
}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def same(left, right):
    if type(left) is not type(right):
        return False
    if isinstance(left, dict):
        return left.keys() == right.keys() and all(same(left[k], right[k]) for k in left)
    if isinstance(left, list):
        return len(left) == len(right) and all(same(a, b) for a, b in zip(left, right))
    return left == right


def environment(items):
    if items is not None and type(items) is not list:
        raise Failure("typed complete environment")
    result = {}
    for item in items or []:
        if type(item) is not str or "=" not in item:
            raise Failure("typed environment entry")
        key, value = item.split("=", 1)
        if key in result:
            raise Failure("duplicate environment key")
        result[key] = value
    return result


def mount_invariants(items):
    result = {}
    for mount in items:
        if mount["Type"] not in ("bind", "volume") or type(mount["RW"]) is not bool:
            raise Failure("typed admitted mount")
        target = mount["Destination"]
        if target in result:
            raise Failure("duplicate native mount destination")
        key = "Name" if mount["Type"] == "volume" else "Source"
        result[target] = {"Type": mount["Type"], key: mount[key], "RW": mount["RW"]}
    return result


def rendered_bytes(value):
    if type(value) is int and value > 0:
        return value
    if type(value) is str and re.fullmatch(r"[1-9][0-9]*", value):
        return int(value)
    raise Failure("positive decimal rendered memory")


class Runner:
    def __init__(self, output, active=90, cleanup=30, *, start=None, utc_start=None,
                 sources=()):
        self.start = time.monotonic() if start is None else start
        self.utc_start = time.time() if utc_start is None else utc_start
        self.work_end = self.start + active
        self.end = self.work_end + cleanup
        self.work_utc_end = self.utc_start + active
        self.utc_end = self.work_utc_end + cleanup
        if ".." in Path(output).parts:
            raise Failure("canonical owned output path")
        self.output = Path(os.path.abspath(output))
        for source in sources:
            source = Path(os.path.abspath(source))
            if self.output == source or source in self.output.parents or self.output in source.parents:
                raise Failure("output/source separation before writes")
        with intake_deadline(self.work_end, self.work_utc_end) as check, ExitStack() as held:
            bound_sources = []
            for source in dict.fromkeys(map(os.path.abspath, sources)):
                fd = held.enter_context(physical_open(source, directory=None, check=check))
                location, info = physical_location(source, fd), os.fstat(fd)
                if any(location == prior_location or (info.st_dev, info.st_ino)
                       == (prior_info.st_dev, prior_info.st_ino)
                       for prior_location, prior_info in bound_sources):
                    raise Failure("duplicate physical source before writes")
                bound_sources.append((location, info))
            parent = held.enter_context(physical_open(self.output.parent, directory=True, check=check))
            device, backing = physical_location(self.output.parent, parent)
            output_backing = backing / self.output.name
            for (source_device, source_backing), info in bound_sources:
                if device == source_device and (
                        output_backing == source_backing or output_backing in source_backing.parents
                        or (stat.S_ISDIR(info.st_mode) and source_backing in output_backing.parents)):
                    raise Failure("physical output/source separation before writes")
            check()
            os.mkdir(self.output.name, dir_fd=parent)
            check()
            os.fsync(parent)
            with physical_open(self.output, directory=True, check=check) as fd:
                info = os.fstat(fd)
                self.output_identity = (info.st_dev, info.st_ino)
        self.records = []
        self.pending = []
        self.pending_writers = []
        self.first = None
        self.later_faults = []
        self.owned = []
        self.unresolved_resources = False

    def fault(self, reason):
        if self.first is None:
            self.first = reason
        else:
            self.later_faults.append(reason)

    def publication_deadline(self, cleanup=False, *, end=None, utc_end=None):
        limit = self.end if cleanup else self.work_end
        utc_limit = self.utc_end if cleanup else self.work_utc_end
        if end is not None:
            limit = min(limit, end)
        if utc_end is not None:
            utc_limit = min(utc_limit, utc_end)
        if time.monotonic() >= limit or time.time() >= utc_limit:
            reason = "native publication deadline" if end is not None else "original publication deadline"
            self.fault(reason)
            raise Failure(reason)
        return limit, utc_limit

    def save(self, name, raw, *, cleanup=False, end=None, utc_end=None):
        if self.pending_writers:
            raise Failure("unresolved writer custody forbids publication")
        limit, utc_limit = self.publication_deadline(cleanup, end=end, utc_end=utc_end)
        if Path(name).name != name or name in ("", ".", ".."):
            raise Failure("owned receipt basename")
        try:
            with intake_deadline(limit, utc_limit) as check, physical_open(self.output, directory=True, check=check) as directory:
                info = os.fstat(directory)
                if (info.st_dev, info.st_ino) != self.output_identity:
                    raise Failure("owned output identity changed")
                self.publication_deadline(cleanup, end=end, utc_end=utc_end)
                fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                             0o666, dir_fd=directory)
                self.pending_writers.append(fd)
                try:
                    view = memoryview(raw)
                    while view:
                        self.publication_deadline(cleanup, end=end, utc_end=utc_end)
                        written = os.write(fd, view)
                        if written <= 0:
                            raise Failure("receipt write made no progress")
                        view = view[written:]
                    self.publication_deadline(cleanup, end=end, utc_end=utc_end)
                    os.fsync(fd)
                except Exception:
                    if time.monotonic() >= limit or time.time() >= utc_limit:
                        reason = "native publication deadline" if end is not None else "original publication deadline"
                        self.fault(reason)
                    else:
                        self.fault("receipt storage fault")
                    raise
                finally:
                    primary = sys.exc_info()[1]
                    close_fault = None
                    try:
                        self.publication_deadline(cleanup, end=end, utc_end=utc_end)
                    except Exception as error:
                        close_fault = error
                    try:
                        # Close on failure is custody release, not publication.
                        os.close(fd)
                        self.pending_writers.remove(fd)
                    except Exception as error:
                        self.fault("receipt close custody fault")
                        close_fault = close_fault or error
                    if close_fault and primary is None:
                        raise close_fault
                self.publication_deadline(cleanup, end=end, utc_end=utc_end)
                os.fsync(directory)
            self.publication_deadline(cleanup, end=end, utc_end=utc_end)
        except Exception:
            if time.monotonic() >= limit or time.time() >= utc_limit:
                reason = "native publication deadline" if end is not None else "original publication deadline"
                self.fault(reason)
            else:
                self.fault("receipt storage fault")
            raise

    def call(self, argv, *, cleanup=False, seconds=15, expected=0):
        if type(seconds) not in (int, float) or not math.isfinite(seconds) or seconds <= 0:
            raise Failure("finite positive native duration required")
        if self.pending or self.pending_writers:
            raise Failure("unresolved native custody forbids dispatch")
        limit = self.end if cleanup else self.work_end
        utc_limit = self.utc_end if cleanup else self.work_utc_end
        if time.monotonic() >= limit or time.time() >= utc_limit or (self.first and not cleanup):
            raise Failure("original deadline or prior failure")
        cutoff = min(limit, time.monotonic() + seconds)
        utc_cutoff = min(utc_limit, time.time() + seconds)
        record = {"argv": argv, "pid": None, "exit": None, "eof": [False, False],
                  "first_fault": None, "later_faults": [], "reaped": False,
                  "acquisition": "pending", "readers_closed": [False, False],
                  "selector_closed": False}
        self.records.append(record)
        buffers = [bytearray(), bytearray()]
        process = None
        selector = selectors.DefaultSelector()

        def fault(reason):
            if record["first_fault"] is None:
                record["first_fault"] = reason
                self.fault(reason)
            else:
                record["later_faults"].append(reason)
                self.fault(reason)

        try:
            if os.getuid() != os.geteuid():
                raise Failure("same real/effective native UID required")
            # Keep the real parent readers independent of constructor cleanup.
            # Only the acquiring worker waits for exec; the observer stays here.
            process = subprocess.Popen.__new__(subprocess.Popen)
            process.pid, process.returncode, process._child_created = None, None, False
            process._qa_readers, process._qa_reader_fds, process._qa_writers = [], [], []
            process._qa_selector = selector
            process._qa_done, process._qa_error = threading.Event(), None
            process._qa_no_birth, process._qa_record = False, record
            self.pending.append(process)
            with intake_deadline(cutoff, utc_cutoff) as check:
                for index in range(2):
                    check()
                    reader, writer = os.pipe()
                    process._qa_reader_fds.append(reader)
                    process._qa_writers.append(writer)
                    check()
                    stream = os.fdopen(reader, "rb", buffering=0)
                    process._qa_readers.append(stream)
                    check()
                    os.set_blocking(stream.fileno(), False)
                    selector.register(stream, selectors.EVENT_READ, index)

            def acquire():
                try:
                    # Explicit unchanged UID selects fork, not CPython's vfork:
                    # it does not synchronously wait for the child's exec. This
                    # does not claim a universal kernel scheduling guarantee.
                    if time.monotonic() >= cutoff or time.time() >= utc_cutoff:
                        process._qa_no_birth = True
                        raise Failure("native birth deadline")
                    subprocess.Popen.__init__(process, argv,
                                              stdout=process._qa_writers[0],
                                              stderr=process._qa_writers[1],
                                              stdin=subprocess.DEVNULL,
                                              start_new_session=True, user=os.geteuid())
                except BaseException as error:
                    process._qa_error = error
                finally:
                    process._qa_done.set()

            process._qa_acquiring_thread = threading.Thread(target=acquire, daemon=False)
            process._qa_acquiring_thread.start()
            killed = False
            acquired = False
            acquisition_failed = False
            while selector.get_map() or not acquired or process.poll() is None:
                now = time.monotonic()
                if now >= cutoff or time.time() >= utc_cutoff:
                    if not acquired:
                        record["acquisition"] = "not-started" if process._qa_no_birth else "unknown"
                        record["pid"] = process.pid
                    fault("unresolved native custody" if acquired else "native birth deadline")
                    break
                if (not acquired and not acquisition_failed and process._qa_done.is_set()
                        and not process._qa_acquiring_thread.is_alive()):
                    record["pid"] = process.pid
                    with intake_deadline(cutoff, utc_cutoff) as check:
                        for writer in list(process._qa_writers):
                            try:
                                check()
                                os.close(writer)
                                process._qa_writers.remove(writer)
                            except OSError:
                                fault("native writer close custody fault")
                    if process._qa_error is not None:
                        record["acquisition_error"] = "native birth deadline" if process._qa_no_birth else "native constructor failure"
                        record["acquisition"] = "not-started" if process._qa_no_birth else "unknown"
                        acquisition_failed = True
                        fault("native birth deadline" if process._qa_no_birth else "native acquisition failed")
                    else:
                        acquired = True
                        record["acquisition"] = "complete"
                        process.stdout, process.stderr = process._qa_readers
                code = process.poll() if acquired else None
                if code is not None and record["exit"] is None:
                    record["exit"] = code
                    record["reaped"] = True
                    if code != expected:
                        fault("unexpected native exit")
                if acquired and (now >= cutoff - 0.25 or time.time() >= utc_cutoff - 0.25) and not killed:
                    fault("native deadline")
                    # Popen owns an unreaped living session leader. Never signal a
                    # recycled group number after the leader's kernel reap.
                    if process.poll() is None:
                        try:
                            os.killpg(process.pid, signal.SIGKILL)
                        except ProcessLookupError:
                            pass
                    killed = True
                for key, _ in selector.select(min(0.05, max(0, cutoff - now))):
                    index = key.data
                    try:
                        raw = os.read(key.fd, 16384)
                    except OSError:
                        fault("native reader fault")
                        selector.unregister(key.fileobj)
                        continue
                    if not raw:
                        record["eof"][index] = True
                        selector.unregister(key.fileobj)
                    else:
                        room = max(0, 131072 - sum(map(len, buffers)))
                        buffers[index].extend(raw[:room])
                        if len(raw) > room:
                            fault("native capture overflow")
                if acquisition_failed and not selector.get_map():
                    break
            code = process.poll() if acquired else None
            if code is not None:
                exit_was_observed = record["exit"] is not None
                record["exit"] = code
                record["reaped"] = True
                if code != expected and not exit_was_observed:
                    fault("unexpected native exit")
            if record["reaped"] and all(record["eof"]):
                for index, stream in enumerate(process._qa_readers):
                    try:
                        with intake_deadline(cutoff, utc_cutoff) as check:
                            check()
                            fd = stream.fileno()
                            check()
                            stream.close()
                        record["readers_closed"][index] = True
                        process._qa_reader_fds.remove(fd)
                    except Exception:
                        fault("native reader close custody fault")
            record["released"] = (record["reaped"] and all(record["eof"])
                                  and all(record["readers_closed"])
                                  and not process._qa_writers)
        except Exception:
            if record["acquisition"] != "complete":
                record["acquisition"] = "not-started" if getattr(process, "_qa_no_birth", False) else "unknown"
                record["pid"] = getattr(process, "pid", None)
                if time.monotonic() >= cutoff or time.time() >= utc_cutoff:
                    fault("native birth deadline")
            fault("native dispatch or capture fault")
            record["released"] = process is None
        finally:
            # Register immutable raw bytes before either fallible publication.
            record["raw"] = tuple(bytes(part) for part in buffers)
            self._release_no_birth(process)
            try:
                if not record["selector_closed"]:
                    with intake_deadline(cutoff, utc_cutoff) as check:
                        check()
                        selector.close()
                record["selector_closed"] = True
                if record.get("released") and process in self.pending:
                    self.pending.remove(process)
            except Exception:
                fault("native selector close custody fault")
                record["released"] = False
            number = len(self.records)
            for index, channel in enumerate(("stdout", "stderr")):
                try:
                    self.save(f"{number:03d}.{channel}", record["raw"][index], cleanup=True,
                              end=cutoff, utc_end=utc_cutoff)
                except Exception:
                    if time.monotonic() >= cutoff or time.time() >= utc_cutoff:
                        fault("native publication deadline")
                    fault("native receipt storage fault")
            record["sha256"] = [digest(part) for part in record["raw"]]
            if time.monotonic() >= cutoff or time.time() >= utc_cutoff:
                fault("native publication deadline")
        if record["first_fault"]:
            raise Failure(record["first_fault"])
        return record["raw"][0]

    def _release_no_birth(self, process):
        if (process is None or not getattr(process, "_qa_no_birth", False)
                or not process._qa_done.is_set() or process._qa_acquiring_thread.is_alive()):
            return
        record = process._qa_record
        record["acquisition"] = "not-started"
        record["no_native_birth"] = True
        # Only the pre-constructor branch proves these descriptors have no child.
        # Closing known owned preparation handles is release, not late execution.
        for fd in list(process._qa_writers):
            try:
                os.close(fd)
                process._qa_writers.remove(fd)
            except OSError:
                record["later_faults"].append("native writer close custody fault")
                self.fault("native writer close custody fault")
        for index, stream in enumerate(process._qa_readers):
            if record["readers_closed"][index]:
                continue
            try:
                fd = stream.fileno()
                stream.close()
                process._qa_reader_fds.remove(fd)
                record["readers_closed"][index] = True
            except Exception:
                record["later_faults"].append("native reader close custody fault")
                self.fault("native reader close custody fault")
        if not record["selector_closed"]:
            try:
                process._qa_selector.close()
                record["selector_closed"] = True
            except Exception:
                record["later_faults"].append("native selector close custody fault")
                self.fault("native selector close custody fault")
        record["released"] = (not process._qa_writers and not process._qa_reader_fds
                              and record["selector_closed"])
        if record["released"] and process in self.pending:
            self.pending.remove(process)

    def docker(self, *args, **kwargs):
        return self.call(["docker", *args], **kwargs)

    def inspect(self, identity, cleanup=False):
        return json.loads(self.docker("inspect", identity, cleanup=cleanup))[0]

    def finish(self):
        for process in list(self.pending):
            self._release_no_birth(process)
        try:
            self.publication_deadline(cleanup=True)
        except Failure:
            pass
        # This immutable snapshot cannot certify its own publication completion.
        # Only the clock-checked return code below can accept the outcome.
        terminal = {"pass": None, "eligible_before_publication": self.first is None,
                    "publication_status": "pre-publication snapshot; actual exit required",
                    "failure": self.first,
                    "later_faults": self.later_faults,
                    "elapsed": time.monotonic() - self.start,
                    "utc_start": self.utc_start, "work_utc_end": self.work_utc_end,
                    "cleanup_utc_end": self.utc_end,
                    "unresolved": [p.pid for p in self.pending],
                    "unresolved_native_readers": [list(getattr(p, "_qa_reader_fds", [])) for p in self.pending],
                    "unresolved_native_writers": [list(getattr(p, "_qa_writers", [])) for p in self.pending],
                    "unresolved_acquisition": [bool(getattr(p, "_qa_acquiring_thread", None)
                                                     and p._qa_acquiring_thread.is_alive()) for p in self.pending],
                    "unresolved_writers": list(self.pending_writers),
                    "unresolved_resources": self.unresolved_resources,
                    "commands": [{k: v for k, v in r.items() if k != "raw"}
                                 for r in self.records]}
        try:
            self.save("terminal.json", json.dumps(terminal, sort_keys=True).encode(), cleanup=True)
        except Exception:
            self.fault("terminal storage fault")
        finally:
            try:
                self.publication_deadline(cleanup=True)
            except Failure:
                pass
            if self.pending or self.pending_writers or self.unresolved_resources:
                # Preserve owning host/handles, without commands, reads or recovery.
                while True:
                    time.sleep(1)
        return 1 if self.first else 0


@contextmanager
def intake_deadline(end, utc_end):
    """Interrupt Linux filesystem intake at the existing dual-clock cutoff."""
    previous = signal.getsignal(signal.SIGALRM)
    if signal.getitimer(signal.ITIMER_REAL) != (0.0, 0.0):
        raise Failure("intake cannot replace an active alarm")

    def check(_signal=None, _frame=None):
        remaining = min(end - time.monotonic(), utc_end - time.time())
        if remaining <= 0:
            raise Failure("original intake deadline")
        signal.setitimer(signal.ITIMER_REAL, min(0.05, remaining), 0.05)

    signal.signal(signal.SIGALRM, check)
    try:
        check()
        yield check
        check()
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous)


@contextmanager
def physical_open(path, *, directory=False, check=None):
    """Keep every no-link directory component bound while consuming a file/root."""
    if ".." in Path(path).parts:
        raise Failure("canonical physical input path")
    parts = Path(os.path.abspath(path)).parts[1:]
    if check:
        check()
    opened = [os.open("/", os.O_RDONLY | os.O_DIRECTORY)]
    bindings = []
    try:
        for index, name in enumerate(parts):
            parent = opened[-1]
            is_directory = index < len(parts) - 1 or directory is True
            flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK
            if is_directory:
                flags |= os.O_DIRECTORY
            if check:
                check()
            fd = os.open(name, flags, dir_fd=parent)
            opened.append(fd)
            if check:
                check()
            info = os.fstat(fd)
            if not is_directory and not (
                    stat.S_ISREG(info.st_mode) and info.st_nlink == 1
                    or directory is None and stat.S_ISDIR(info.st_mode)):
                raise Failure("regular single-link physical input")
            bindings.append((parent, name, info.st_dev, info.st_ino, info.st_mode))
        yield opened[-1]
        for parent, name, device, inode, mode in bindings:
            if check:
                check()
            named = os.stat(name, dir_fd=parent, follow_symlinks=False)
            if ((named.st_dev, named.st_ino, named.st_mode) != (device, inode, mode)
                    or (stat.S_ISREG(mode) and named.st_nlink != 1)):
                raise Failure("physical input component changed")
    finally:
        primary = sys.exc_info()[1]
        close_fault = None
        for fd in reversed(opened):
            try:
                if check:
                    check()
            except Exception as error:
                close_fault = close_fault or error
            try:
                os.close(fd)
            except Exception as error:
                close_fault = close_fault or error
        if close_fault and primary is None:
            raise close_fault


def physical_location(path, fd):
    """Resolve a held inode through Linux mount roots, including bind aliases."""
    path = Path(os.path.abspath(path))
    info = os.fstat(fd)
    device = f"{os.major(info.st_dev)}:{os.minor(info.st_dev)}"
    with open("/proc/self/mountinfo", "rb") as source:
        raw = source.read(1048577)
    if len(raw) > 1048576:
        raise Failure("bounded kernel mount inventory")

    def unescape(value):
        return re.sub(r"\\([0-7]{3})", lambda match: chr(int(match[1], 8)), value)

    mounts = []
    for line in raw.decode().splitlines():
        fields = line.split(" - ", 1)[0].split()
        if len(fields) < 6:
            raise Failure("kernel mount inventory shape")
        root, point = Path(unescape(fields[3])), Path(unescape(fields[4]))
        if path == point or point in path.parents:
            mounts.append((len(point.parts), fields[2], root, point))
    if not mounts:
        raise Failure("physical mount location unavailable")
    _, mounted_device, root, point = max(mounts)
    if mounted_device != device:
        raise Failure("held physical mount identity changed")
    return device, root / path.relative_to(point)


def read_bytes_pinned(path, sha, maximum=1048576):
    with physical_open(path) as fd:
        if os.fstat(fd).st_size > maximum:
            raise Failure("input pin or size")
        chunks, size = [], 0
        while size <= maximum:
            part = os.read(fd, min(65536, maximum + 1 - size))
            if not part:
                break
            chunks.append(part)
            size += len(part)
        raw = b"".join(chunks)
    if len(raw) > maximum or digest(raw) != sha:
        raise Failure("input pin or size")
    return raw


def read_pinned(path, sha):
    raw = read_bytes_pinned(path, sha)
    return json.loads(raw)


def owned_profile(profile, project, service, owner):
    labels = profile["Config"]["Labels"]
    return (labels.get("com.docker.compose.project") == project
            and labels.get("com.docker.compose.service") == service
            and labels.get("synthetic.owner") == owner)


def constructor(profile, expected, project, service, owner):
    if not owned_profile(profile, project, service, owner) or profile["Image"] != expected["image"]:
        raise Failure("exact owned constructor identity")
    if profile["Name"] != "/" + expected["name"]:
        raise Failure("exact constructor name")
    if not REQUIRED_INVARIANTS <= expected["invariants"].keys():
        raise Failure("complete mandatory constructor contract")
    for key, value in expected["invariants"].items():
        actual = profile
        for part in key.split("."):
            actual = actual[part]
        if key == "Mounts":
            actual, value = mount_invariants(actual), mount_invariants(value)
        elif key == "Config.Env":
            actual, value = environment(actual), environment(value)
        if not same(actual, value):
            raise Failure("actual constructor invariant")
    network = expected["network_contract"]
    ports = profile["HostConfig"].get("PortBindings")
    if ports is None:
        ports = {}
    if not same(ports, network["published_ports"]):
        raise Failure("complete loopback native port bindings")
    actual_names = sorted(profile["NetworkSettings"]["Networks"])
    if network["mode"] == "none":
        if (network["complete_owned_names"] or network["native_primary_allowed_names"]
                or profile["HostConfig"]["NetworkMode"] != "none" or actual_names not in ([], ["none"])):
            raise Failure("isolated native network")
    elif (network["mode"] != "owned" or not same(actual_names, network["complete_owned_names"])
          or not same(network["native_primary_allowed_names"], actual_names)
          or profile["HostConfig"]["NetworkMode"] not in actual_names):
        raise Failure("complete owned native networks")


def plan(services, project):
    cpu, memory = 100000000, 134217728  # Trusted operator's own slot allocation.
    for service, expected in services.items():
        if not re.fullmatch(r"sha256:[0-9a-f]{64}", expected["image"]):
            raise Failure("immutable image required")
        if not expected["name"].startswith(project + "-"):
            raise Failure("owned constructor namespace")
        inv = expected["invariants"]
        if not REQUIRED_INVARIANTS <= inv.keys():
            raise Failure("complete mandatory constructor contract")
        for key in ("HostConfig.NanoCpus", "HostConfig.Memory", "HostConfig.MemorySwap",
                    "HostConfig.PidsLimit"):
            if type(inv[key]) is not int or inv[key] <= 0:
                raise Failure("finite typed resource limits")
        if (inv["HostConfig.Privileged"] is not False
                or type(inv["HostConfig.ReadonlyRootfs"]) is not bool
                or inv["HostConfig.CapAdd"] not in (None, [])
                or inv["HostConfig.SecurityOpt"] not in (["no-new-privileges"], ["no-new-privileges:true"])
                or inv["HostConfig.RestartPolicy"] not in (
                    {"Name": "no", "MaximumRetryCount": 0},
                    {"Name": "unless-stopped", "MaximumRetryCount": 0})):
            raise Failure("original security boundary")
        if service != "postgres" and (inv["HostConfig.ReadonlyRootfs"] is not True
                                      or inv["HostConfig.CapDrop"] != ["ALL"]):
            raise Failure("managed runtime role isolation")
        for mount in inv["Mounts"]:
            authority = mount["Source"] if mount["Type"] == "bind" else mount["Name"]
            if "docker.sock" in authority or "docker.sock" in mount["Destination"]:
                raise Failure("no operator socket in graph")
        cpu += inv["HostConfig.NanoCpus"]
        memory += inv["HostConfig.Memory"]
    if cpu > 2000000000 or memory > 4294967296:
        raise Failure("whole graph including operator exceeds slot")


def rendered_guard(service, expected, image, config):
    """Compare the complete admitted render and its native security projection before birth."""
    if not same(service, expected["rendered"]):
        raise Failure("complete rendered service contract")
    if image["Id"] != expected["image"]:
        raise Failure("local immutable image identity")
    inv = expected["invariants"]
    defaults = image["Config"]
    env = dict(item.split("=", 1) for item in defaults.get("Env", []) or [])
    env.update(service.get("environment", {}))
    declared_env = inv["Config.Env"]
    if len(declared_env) != len({item.split("=", 1)[0] for item in declared_env}):
        raise Failure("duplicate native environment name")
    if not same(env, dict(item.split("=", 1) for item in declared_env)):
        raise Failure("rendered environment delivery")
    entry = service.get("entrypoint")
    command = service.get("command")
    projection = {
        "Config.User": service.get("user", defaults.get("User", "")),
        "Config.Entrypoint": defaults.get("Entrypoint") if entry is None else entry,
        "Config.Cmd": (defaults.get("Cmd") if entry is None else None) if command is None else command,
        "HostConfig.NanoCpus": int(Decimal(str(service["cpus"])) * Decimal(1000000000)),
        "HostConfig.Memory": rendered_bytes(service["mem_limit"]),
        "HostConfig.MemorySwap": rendered_bytes(service["memswap_limit"]),
        "HostConfig.PidsLimit": service["pids_limit"],
        "HostConfig.Privileged": service.get("privileged", False),
        "HostConfig.ReadonlyRootfs": service.get("read_only", False),
        "HostConfig.CapAdd": service.get("cap_add"),
        "HostConfig.CapDrop": service.get("cap_drop"),
        "HostConfig.SecurityOpt": service.get("security_opt"),
        "HostConfig.RestartPolicy": {"Name": service.get("restart", "no"), "MaximumRetryCount": 0},
    }
    mismatches = [key for key, value in projection.items() if not same(value, inv[key])]
    if mismatches:
        raise Failure("rendered security/resources/command disagree with native plan: " + ",".join(mismatches))
    if service.get("tmpfs"):
        tmpfs = dict(item.split(":", 1) for item in service["tmpfs"])
        if not same(tmpfs, inv.get("HostConfig.Tmpfs")):
            raise Failure("rendered tmpfs delivery")
    if service.get("group_add") and not same(service["group_add"], inv.get("HostConfig.GroupAdd")):
        raise Failure("rendered additional group authority")
    if any(service.get(key) for key in ("devices", "volumes_from", "pid", "ipc", "uts")):
        raise Failure("no borrowed devices/namespaces/mounts")
    networks = {config["networks"][name]["name"] for name in service.get("networks", {})}
    mode = service.get("network_mode")
    admitted = expected["network_contract"]
    ports = {}
    for port in service.get("ports", []):
        if (port.get("host_ip") != "127.0.0.1" or type(port["target"]) is not int
                or not 1 <= port["target"] <= 65535 or not re.fullmatch(r"[1-9][0-9]*", str(port["published"]))
                or not 1 <= int(port["published"]) <= 65535):
            raise Failure("finite loopback rendered ports")
        key = str(port["target"]) + "/" + port.get("protocol", "tcp")
        ports.setdefault(key, []).append({"HostIp": "127.0.0.1", "HostPort": str(port["published"])})
    if not same(ports, admitted["published_ports"]):
        raise Failure("complete rendered port bindings")
    if mode == "none":
        if (networks or admitted["mode"] != "none" or admitted["complete_owned_names"]
                or admitted["native_primary_allowed_names"]):
            raise Failure("exact isolated role network")
    elif (mode or not networks or admitted["mode"] != "owned"
          or not same(admitted["complete_owned_names"], sorted(networks))
          or not same(admitted["native_primary_allowed_names"], sorted(networks))):
        raise Failure("rendered owned network mode")
    mounts = {mount["Destination"]: mount for mount in inv["Mounts"]}
    if len(mounts) != len(inv["Mounts"]):
        raise Failure("unique admitted mount destinations")
    delivered = {}
    for mount in service.get("volumes", []):
        target = mount["target"]
        if target in delivered:
            raise Failure("duplicate rendered mount target")
        if mount["type"] == "bind":
            if mount.get("bind", {}).get("create_host_path", True):
                raise Failure("no daemon-created bind source")
            delivered[target] = ("bind", mount["source"], not mount.get("read_only", False))
        elif mount["type"] == "volume" and mount.get("source") in config["volumes"]:
            delivered[target] = ("volume", config["volumes"][mount["source"]]["name"], not mount.get("read_only", False))
        else:
            raise Failure("no anonymous or undeclared mount")
    for secret in service.get("secrets", []):
        target = "/run/secrets/" + secret.get("target", secret["source"])
        if target in delivered:
            raise Failure("duplicate rendered secret target")
        delivered[target] = ("bind", config["secrets"][secret["source"]]["file"], False)
    if set(delivered) != set(mounts):
        raise Failure("complete rendered mount set")
    for target, (kind, source, rw) in delivered.items():
        native = mounts[target]
        if native["Type"] != kind or not same(native["RW"], rw):
            raise Failure("rendered mount access/type")
        if native["Name" if kind == "volume" else "Source"] != source:
            raise Failure("rendered daemon source delivery")
    if not set(defaults.get("Volumes") or {}) <= set(delivered):
        raise Failure("no implicit image anonymous volume")


def resource_names(runner, kind, name, cleanup=False):
    return runner.docker(kind, "ls", "--filter", "name=" + name,
                         "--format", "{{.Name}}", cleanup=cleanup).decode().splitlines()


def resource_guard(profile, expected, project, owner, kind):
    if profile["Name"] != expected["name"] or profile["Driver"] != expected["driver"]:
        raise Failure("exact born resource identity/driver")
    labels = profile.get("Labels") or {}
    if labels.get("com.docker.compose.project") != project or labels.get("synthetic.owner") != owner:
        raise Failure("exact born resource ownership")
    actual_options = profile.get("Options")
    if actual_options is None or (type(actual_options) is dict and not actual_options):
        actual_options = None
    if not same(actual_options, expected["options"]):
        raise Failure("exact resource options")
    if kind == "network":
        if profile["Internal"] is not expected["internal"] or profile.get("Containers"):
            raise Failure("internal unused owned network")
    elif profile["Scope"] != "local":
        raise Failure("local owned volume")


def literal_compose(value):
    if isinstance(value, str):
        return value.replace("$", "$$")
    if isinstance(value, list):
        return [literal_compose(item) for item in value]
    if isinstance(value, dict):
        return {key: literal_compose(item) for key, item in value.items()}
    return value


def delivery_snapshot(runner, approval, required, *, cleanup=False):
    if approval.get("no_host_source_writers") is not True:
        raise Failure("independent source writer exclusion")
    files = approval["delivery_files"]
    directories = approval["delivery_directories"]
    if not files or len(files) > 4096 or len(directories) > 256:
        raise Failure("finite admitted delivery manifest")
    by_path = {item["path"]: item for item in files}
    roots = {item["path"]: item for item in directories}
    if len(by_path) != len(files) or len(roots) != len(directories) or not required <= by_path.keys() | roots.keys():
        raise Failure("complete unique delivered sources")
    observed = []
    file_identities, directory_identities = set(), set()
    limit = runner.end if cleanup else runner.work_end
    utc_limit = runner.utc_end if cleanup else runner.work_utc_end
    for path, item in by_path.items():
        if not path.startswith(DAEMON_SOURCE_PREFIX):
            raise Failure("admitted daemon source path")
        with intake_deadline(limit, utc_limit), physical_open(path) as fd:
            before = os.fstat(fd)
            identity = (before.st_dev, before.st_ino)
            if identity in file_identities:
                raise Failure("duplicate physical delivered file")
            file_identities.add(identity)
            if (not stat.S_ISREG(before.st_mode) or before.st_nlink != 1
                    or type(item["bytes"]) is not int or not 0 <= item["bytes"] <= 134217728
                    or before.st_size != item["bytes"]):
                raise Failure("bounded regular delivered file")
            checksum = hashlib.sha256()
            while True:
                if time.monotonic() >= limit or time.time() >= utc_limit:
                    raise Failure("original delivery deadline")
                part = os.read(fd, 1048576)
                if not part:
                    break
                checksum.update(part)
            after = os.fstat(fd)
            fields = ("st_dev", "st_ino", "st_size", "st_mtime_ns", "st_ctime_ns")
            named = os.stat(path, follow_symlinks=False)
            if (any(getattr(before, key) != getattr(after, key) for key in fields)
                    or not stat.S_ISREG(named.st_mode) or named.st_dev != before.st_dev
                    or named.st_ino != before.st_ino or checksum.hexdigest() != item["sha256"]):
                raise Failure("actual delivered bytes or identity")
            observed.append({"path": path, "bytes": before.st_size, "sha256": checksum.hexdigest(),
                             "device": before.st_dev, "inode": before.st_ino,
                             "mtime_ns": before.st_mtime_ns, "ctime_ns": before.st_ctime_ns})
    for path, item in roots.items():
        if not path.startswith(DAEMON_SOURCE_PREFIX):
            raise Failure("admitted delivery directory path")
        members, subdirs = [], []
        with intake_deadline(limit, utc_limit), physical_open(path, directory=True) as root_fd:
            info = os.fstat(root_fd)
            identity = (info.st_dev, info.st_ino)
            if identity in directory_identities:
                raise Failure("duplicate physical delivered directory")
            directory_identities.add(identity)
            for parent, children, names, directory_fd in os.fwalk(".", follow_symlinks=False, dir_fd=root_fd):
                if time.monotonic() >= limit or time.time() >= utc_limit:
                    raise Failure("original delivery inventory deadline")
                for name in children + names:
                    relative = (Path(parent) / name).as_posix()
                    mode = os.stat(name, dir_fd=directory_fd, follow_symlinks=False).st_mode
                    if stat.S_ISDIR(mode):
                        subdirs.append(relative)
                    elif stat.S_ISREG(mode):
                        members.append(relative)
                        if str(Path(path) / relative) not in by_path:
                            raise Failure("unadmitted directory member bytes")
                    else:
                        raise Failure("no delivered links or specials")
        if not same(sorted(members), item["members"]) or not same(sorted(subdirs), item["directories"]):
            raise Failure("exact delivery directory topology")
    return observed


D_SOURCE_VERIFY = """import hashlib,importlib.machinery,json,os,stat,sys,types
manifest=json.loads(sys.argv[1])
main_path='/input/test-maintenance.py'
roots=('/input','/transport')
if set(manifest)!={main_path,'/input/run-maintenance.py','/transport/run-qualified.py','/transport/guards.py'}:
    raise RuntimeError('exact four delivered executable sources required')
def proof():
    result,snapshots={},{}
    for path,sha in manifest.items():
        fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW)
        with os.fdopen(fd,'rb') as source:
            info=os.fstat(source.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_size>131072:
                raise RuntimeError('regular bounded delivered source required')
            raw=source.read(131073)
        actual=hashlib.sha256(raw).hexdigest()
        if actual!=sha:
            raise RuntimeError('delivered source hash mismatch')
        result[path]=actual
        snapshots[path]=raw
    return result,snapshots
before,snapshots=proof()
source_loader=importlib.machinery.SourceFileLoader
bytecode_loader=importlib.machinery.SourcelessFileLoader
source_read,bytecode_read=source_loader.get_data,bytecode_loader.get_data
def authenticated_data(loader,path):
    path=os.path.abspath(path)
    if path in snapshots:
        return snapshots[path]
    if any(path.startswith(root+os.sep) for root in roots):
        if path.endswith('.pyc'):
            raise FileNotFoundError('local bytecode is not admitted')
        raise RuntimeError('local source is not admitted')
    return source_read(loader,path)
main=types.ModuleType('__main__')
main.__file__,main.__package__,main.__spec__=main_path,None,None
previous_main=sys.modules.get('__main__')
sys.argv=[main_path]
execution_error=post_error=None
try:
    source_loader.get_data=bytecode_loader.get_data=authenticated_data
    sys.modules['__main__']=main
    try:
        exec(compile(snapshots[main_path],main_path,'exec'),main.__dict__)
    except BaseException as error:
        if not isinstance(error,SystemExit) or not (error.code is None or
                isinstance(error.code,int) and error.code==0):
            execution_error=error
finally:
    source_loader.get_data,bytecode_loader.get_data=source_read,bytecode_read
    if previous_main is None:
        sys.modules.pop('__main__',None)
    else:
        sys.modules['__main__']=previous_main
    try:
        after,_=proof()
        if before!=after:
            raise RuntimeError('delivered source changed')
    except BaseException as error:
        post_error=error
if execution_error is not None:
    raise execution_error from post_error
if post_error is not None:
    raise post_error
print('D001_DELIVERED_SOURCE_PROOF '+json.dumps(after,sort_keys=True),flush=True)
"""


def compose_config(runner, approval):
    project = approval["project"]
    if not re.fullmatch(r"synthetic-qa-[a-z0-9-]{1,48}", project):
        raise Failure("synthetic project identity")
    if runner.docker("info", "--format", "{{.ID}}").decode().strip() != approval["daemon_id"]:
        raise Failure("daemon identity")
    compose = approval["compose_file"]
    with intake_deadline(runner.work_end, runner.work_utc_end):
        read_bytes_pinned(compose, approval["compose_sha256"])
    command = ["compose", "--project-name", project, "--file", compose]
    raw = runner.docker(*command, "--profile", "maintenance", "config", "--format", "json")
    config = json.loads(raw)
    runner.delivery_sources = {
        mount["source"] for name, service in config.get("services", {}).items()
        if name in approval.get("services", {}) for mount in service.get("volumes", [])
        if mount["type"] == "bind"
    } | {
        config["secrets"][secret["source"]]["file"]
        for name, service in config.get("services", {}).items() if name in approval.get("services", {})
        for secret in service.get("secrets", [])
    }
    runner.save("rendered-compose.json", raw)
    if approval["operation"] == "C_RENDER":
        return None
    if digest(raw) != approval["rendered_config_sha256"]:
        raise Failure("rendered Compose pin")
    services = approval["services"]
    owner = approval["owner"]
    plan(services, project)
    inactive = approval["inactive_services"]
    if set(config["services"]) != set(services) | set(inactive) or set(services) & set(inactive):
        raise Failure("exact service set")
    for name, service in config["services"].items():
        if name in inactive:
            if service.get("profiles") != ["maintenance"]:
                raise Failure("inactive maintenance profile")
            continue
        if service.get("image") != services[name]["image"]:
            raise Failure("service image identity")
        if service.get("labels", {}).get("synthetic.owner") != owner:
            raise Failure("service owner identity")
        if service.get("container_name", project + "-" + name + "-1") != services[name]["name"]:
            raise Failure("explicit owned service name")
        if not set(service.get("depends_on", {})) <= set(services):
            raise Failure("no undeclared dependency creation")
        local_image = json.loads(runner.docker("image", "inspect", services[name]["image"]))[0]
        rendered_guard(service, services[name], local_image, config)
    if any(v.get("external") for v in config.get("volumes", {}).values()):
        raise Failure("no foreign external volume")
    if any(n.get("external") for n in config.get("networks", {}).values()):
        raise Failure("no foreign external network")
    for service in config["services"].values():
        for port in service.get("ports", []):
            if port.get("host_ip") != "127.0.0.1":
                raise Failure("loopback-only published ports")
    resources = []
    for kind, section in (("network", "networks"), ("volume", "volumes")):
        if set(config.get(section, {})) != set(approval[section]):
            raise Failure("complete admitted network/volume set")
        for logical, declared in config.get(section, {}).items():
            expected = approval[section][logical]
            if not same(declared, expected["rendered"]):
                raise Failure("complete rendered resource contract")
            if declared["name"] != expected["name"] or not expected["name"].startswith(project + "_"):
                raise Failure("owned named resource namespace")
            if declared.get("labels", {}).get("synthetic.owner") != owner:
                raise Failure("rendered resource owner")
            options = declared.get("driver_opts") or None
            if declared.get("driver", "bridge" if kind == "network" else "local") != expected["driver"] or not same(options, expected["options"]):
                raise Failure("rendered resource driver/options")
            if kind == "network" and not same(declared.get("internal", False), expected["internal"]):
                raise Failure("rendered network isolation")
            resources.append((kind, expected))
    # CREATE consumes only this complete validated render, not mutable includes
    # or env inputs. Escape dollar values for Compose's second interpolation.
    frozen = json.dumps(literal_compose(config), sort_keys=True).encode()
    runner.save("create-compose.json", frozen)
    runner.create_compose_pin = digest(frozen)
    command = ["compose", "--project-name", project, "--file",
               str(runner.output / "create-compose.json")]
    return project, command, services, resources


def preflight(runner, approval):
    project, command, services, resources = compose_config(runner, approval)
    owner = approval["owner"]
    if runner.docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + project).strip():
        raise Failure("project not fresh")
    for expected in services.values():
        if runner.docker("ps", "-aq", "--filter", "name=^/" + re.escape(expected["name"]) + "$").strip():
            raise Failure("declared constructor name not fresh")
    for kind, expected in resources:
        if expected["name"] in resource_names(runner, kind, expected["name"]):
            raise Failure("declared network/volume not fresh")
    with intake_deadline(runner.work_end, runner.work_utc_end):
        read_pinned(command[-1], runner.create_compose_pin)
    before_delivery = delivery_snapshot(runner, approval, runner.delivery_sources)
    runner.save("delivery-before.json", json.dumps(before_delivery, sort_keys=True).encode())
    try:
        # CREATE may succeed even when its client/receipt fails: cleanup discovers labels.
        runner.owned = list(services)
        runner.unresolved_resources = True
        runner.docker(*command, "create", "--no-build", "--pull", "never", *services)
        ids = runner.docker("ps", "-aq", "--no-trunc", "--filter",
                            "label=com.docker.compose.project=" + project).decode().split()
        if len(ids) != len(services):
            raise Failure("complete Created cohort")
        seen = set()
        for identity in ids:
            if not re.fullmatch(r"[0-9a-f]{64}", identity):
                raise Failure("full container identity")
            profile = runner.inspect(identity)
            if profile["Id"] != identity:
                raise Failure("actual inspected container identity")
            service = profile["Config"]["Labels"].get("com.docker.compose.service")
            if service not in services or service in seen or not owned_profile(profile, project, service, owner):
                raise Failure("owned exact Created service")
            seen.add(service)
            expected = services[service]
            state = profile["State"]
            if state["Status"] != "created" or state["Pid"] != 0 or state["Running"]:
                raise Failure("never-started state")
            constructor(profile, expected, project, service, owner)
            for key in ("Config", "HostConfig", "Mounts"):
                runner.save(service + "-" + key + ".json",
                            json.dumps(profile[key], sort_keys=True).encode())
        for kind, expected in resources:
            profile = json.loads(runner.docker(kind, "inspect", expected["name"]))[0]
            resource_guard(profile, expected, project, owner, kind)
    except Exception as error:
        runner.fault(str(error) if isinstance(error, Failure) else "constructor failure")
        raise
    finally:
        if runner.owned and not runner.pending:
            ids = runner.docker("ps", "-aq", "--no-trunc", "--filter",
                                "label=com.docker.compose.project=" + project, cleanup=True).decode().split()
            if not ids:
                raise Failure("unknown attempted CREATE custody")
            for identity in ids:
                if not re.fullmatch(r"[0-9a-f]{64}", identity):
                    raise Failure("full cleanup container identity")
                profile = runner.inspect(identity, cleanup=True)
                if profile["Id"] != identity:
                    raise Failure("actual inspected cleanup identity")
                service = profile["Config"]["Labels"].get("com.docker.compose.service")
                if service not in services or not owned_profile(profile, project, service, owner):
                    raise Failure("foreign constructor: no removal")
                constructor(profile, services[service], project, service, owner)
                if profile["State"]["Status"] != "created" or profile["State"]["Pid"] != 0:
                    raise Failure("started constructor: no removal")
                runner.docker("rm", identity, cleanup=True)
                if runner.docker("ps", "-aq", "--no-trunc", "--filter", "id=" + identity,
                                 cleanup=True).strip():
                    raise Failure("full-ID absence")
                name = profile["Name"]
                if runner.docker("ps", "-aq", "--no-trunc", "--filter",
                                 "name=^" + re.escape(name) + "$", cleanup=True).strip():
                    raise Failure("anchored-name absence")
            for expected in services.values():
                if runner.docker("ps", "-aq", "--filter",
                                 "name=^/" + re.escape(expected["name"]) + "$", cleanup=True).strip():
                    raise Failure("whole declared name absence")
            for kind, expected in resources:
                if expected["name"] not in resource_names(runner, kind, expected["name"], cleanup=True):
                    continue
                profile = json.loads(runner.docker(kind, "inspect", expected["name"], cleanup=True))[0]
                resource_guard(profile, expected, project, owner, kind)
                if kind == "volume" and runner.docker("ps", "-aq", "--filter", "volume=" + expected["name"], cleanup=True).strip():
                    raise Failure("no volume consumers before owned removal")
                runner.docker(kind, "rm", profile["Id"] if kind == "network" else expected["name"], cleanup=True)
                if expected["name"] in resource_names(runner, kind, expected["name"], cleanup=True):
                    raise Failure("exact named network/volume absence")
            runner.unresolved_resources = False
            after_delivery = delivery_snapshot(runner, approval, runner.delivery_sources, cleanup=True)
            runner.save("delivery-after.json", json.dumps(after_delivery, sort_keys=True).encode(), cleanup=True)
            if not same(before_delivery, after_delivery):
                raise Failure("source delivery changed during preflight")


def maintenance_isolation(host):
    expected = {"NanoCpus": 200000000, "Memory": 268435456, "MemorySwap": 268435456,
                "PidsLimit": 64, "NetworkMode": "none", "ReadonlyRootfs": True,
                "Privileged": False, "CapAdd": None, "CapDrop": ["ALL"],
                "SecurityOpt": ["no-new-privileges"],
                "RestartPolicy": {"Name": "no", "MaximumRetryCount": 0},
                "Tmpfs": {"/tmp": "rw,noexec,nosuid,nodev,size=16m,mode=1777"}}
    if any(not same(host[key], value) for key, value in expected.items()):
        raise Failure("original source helper isolation")


def maintenance_controls(runner, approval):
    project = approval["project"]
    name = project + "-maintenance-controls"
    if not re.fullmatch(r"synthetic-qa-[a-z0-9-]{1,48}", project):
        raise Failure("synthetic source-check namespace")
    if runner.docker("info", "--format", "{{.ID}}").decode().strip() != approval["daemon_id"]:
        raise Failure("daemon identity")
    image = "sha256:c7776d61291b599bb4dd2f30eb6e7f33f2336e3b88ae800cb1d580e01661467e"
    if json.loads(runner.docker("image", "inspect", image))[0]["Id"] != image:
        raise Failure("existing local source image required")
    targets = {"/input/run-maintenance.py", "/input/test-maintenance.py",
               "/transport/run-qualified.py", "/transport/guards.py"}
    inputs = approval["inputs"]
    if {item["target"] for item in inputs} != targets or len(inputs) != 4:
        raise Failure("exact four public source mounts")
    for item in inputs:
        with intake_deadline(runner.work_end, runner.work_utc_end):
            read_bytes_pinned(item["operator_path"], item["sha256"], 131072)
        if not item["daemon_source"].startswith("/run/desktop/mnt/host/c/Users/ddriz/Projects/zns-chatbot/qa.local/"):
            raise Failure("public source daemon namespace")
    if runner.docker("ps", "-aq", "--filter", "name=^/" + name + "$").strip():
        raise Failure("fresh exact source helper")
    delivered_manifest = {item["target"]: item["sha256"] for item in inputs}
    helper_command = ["-B", "-c", D_SOURCE_VERIFY, json.dumps(delivered_manifest, sort_keys=True)]
    argv = ["create", "--pull", "never", "--name", name, "--label", "synthetic.owner=" + project,
            "--cpus", "0.2", "--memory", "256m", "--memory-swap", "256m",
            "--pids-limit", "64", "--network", "none", "--read-only", "--no-healthcheck",
            "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--restart", "no",
            "--user", "1000:1000", "--env", "HOME=/tmp", "--env", "XDG_CACHE_HOME=/tmp/.cache",
            "--env", "PYTHONDONTWRITEBYTECODE=1",
            "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=16m,mode=1777"]
    for item in inputs:
        argv += ["--mount", "type=bind,src=" + item["daemon_source"]
                 + ",dst=" + item["target"] + ",readonly"]
    argv += ["--entrypoint", "/usr/bin/python3", image, *helper_command]

    def guard(profile):
        host = profile["HostConfig"]
        cfg = profile["Config"]
        mounts = profile["Mounts"]
        if (profile["Name"] != "/" + name or profile["Image"] != image
                or cfg["Labels"].get("synthetic.owner") != project
                or cfg["User"] != "1000:1000"
                or cfg["Entrypoint"] != ["/usr/bin/python3"]
                or cfg["Cmd"] != helper_command):
            raise Failure("exact owned source constructor")
        maintenance_isolation(host)
        if len(mounts) != 4:
            raise Failure("four RO source binds only")
        declared = {item["target"]: item["daemon_source"] for item in inputs}
        for mount in mounts:
            if (mount["Type"] != "bind" or mount["RW"] is not False
                    or mount["Propagation"] != "rprivate"
                    or declared.get(mount["Destination"]) != mount["Source"]):
                raise Failure("exact source mount identity")

    attempted = False
    try:
        attempted = True
        runner.unresolved_resources = True
        raw = runner.docker(*argv).decode().strip()
        if not re.fullmatch(r"[0-9a-f]{64}", raw):
            raise Failure("CREATE full-ID response")
        guard(runner.inspect(raw))
        output = runner.docker("start", "--attach", raw,
                               seconds=max(0, runner.work_end - time.monotonic()))
        profile = runner.inspect(raw)
        guard(profile)
        if profile["State"]["Status"] != "exited" or profile["State"]["ExitCode"] != 0:
            raise Failure("source test terminal")
        marker = b"D001_MAINTENANCE_PUBLICATION_RESULT total6 skipped=0 pass=true"
        if output.splitlines().count(marker) != 1:
            raise Failure("complete six controls and zero skips")
        proof = b"D001_DELIVERED_SOURCE_PROOF " + json.dumps(delivered_manifest, sort_keys=True).encode()
        if output.splitlines().count(proof) != 1:
            raise Failure("actual before/after mounted source proof")
    except Exception as error:
        runner.fault(str(error) if isinstance(error, Failure) else "source controls failure")
        raise
    finally:
        if attempted and not runner.pending:
            ids = runner.docker("ps", "-aq", "--no-trunc", "--filter",
                                "name=^/" + name + "$", cleanup=True).decode().split()
            if not ids:
                raise Failure("unknown attempted source CREATE custody")
            if len(ids) > 1:
                raise Failure("ambiguous source helper custody")
            for identity in ids:
                if not re.fullmatch(r"[0-9a-f]{64}", identity):
                    raise Failure("full owned source ID")
                profile = runner.inspect(identity, cleanup=True)
                guard(profile)
                if profile["State"]["Running"]:
                    runner.docker("stop", "--timeout", "2", identity, cleanup=True)
                    profile = runner.inspect(identity, cleanup=True)
                    guard(profile)
                if profile["State"]["Running"] or profile["State"]["Pid"] != 0:
                    raise Failure("owned source helper not stopped")
                runner.docker("rm", identity, cleanup=True)
                if runner.docker("ps", "-aq", "--no-trunc", "--filter", "id=" + identity,
                                 cleanup=True).strip():
                    raise Failure("source full-ID absence")
            if runner.docker("ps", "-aq", "--filter", "name=^/" + name + "$", cleanup=True).strip():
                raise Failure("source anchored-name absence")
            runner.unresolved_resources = False


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--approval", required=True)
    parser.add_argument("--approval-sha256", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    start, utc_start = time.monotonic(), time.time()
    try:
        with intake_deadline(start + 90, utc_start + 90):
            approval = read_pinned(args.approval, args.approval_sha256)
        if approval.get("passive_native_custody_allowed") is not True:
            raise Failure("explicit passive physical host custody required")
        if approval["authority"] != "ROOT" or approval["operation"] not in ("C_CREATED_PREFLIGHT", "C_RENDER", "D_MAINTENANCE_CONTROLS"):
            raise Failure("exact operator scope")
        if time.monotonic() >= start + 90 or time.time() >= utc_start + 90:
            raise Failure("original admission deadline")
    except Exception:
        print("ROOT approval rejected before operator setup", file=sys.stderr)
        return 1
    try:
        sources = [args.approval]
        if "compose_file" in approval:
            sources.append(approval["compose_file"])
        sources.extend(item["path"] for item in approval.get("delivery_files", []))
        sources.extend(item["path"] for item in approval.get("delivery_directories", []))
        sources.extend(item["operator_path"] for item in approval.get("inputs", []))
        runner = Runner(args.output, start=start, utc_start=utc_start, sources=sources)
    except Exception:
        print("ROOT output rejected before operator setup", file=sys.stderr)
        return 1
    try:
        if approval["operation"] == "C_RENDER":
            compose_config(runner, approval)
        elif approval["operation"] == "C_CREATED_PREFLIGHT":
            preflight(runner, approval)
        else:
            maintenance_controls(runner, approval)
    except Exception as error:
        runner.fault(str(error) if isinstance(error, Failure) else "operator failure")
    return runner.finish()


if __name__ == "__main__":
    raise SystemExit(main())
