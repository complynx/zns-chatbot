import importlib.util
import importlib.machinery
import contextlib
import ctypes
import io
import json
import os
import signal
import stat
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("runner", Path(__file__).with_name("run.py"))
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def actual_inputs(manifest):
    """Validate captured producer bytes without native commands or private output."""
    required = {"plan", "render", "images", "postgres_image", "postgres_config", "d_constructor"}
    if set(manifest["inputs"]) != required:
        raise MODULE.Failure("exact six actual producer input set")
    raw = {}
    identities = set()
    for key, item in manifest["inputs"].items():
        with MODULE.physical_open(item["path"]) as fd:
            info = os.fstat(fd)
            identity = (info.st_dev, info.st_ino)
            if identity in identities:
                raise MODULE.Failure("distinct physical actual producer inputs")
            identities.add(identity)
        raw[key] = MODULE.read_bytes_pinned(item["path"], item["sha256"])
    current = json.loads(raw["plan"])
    render = json.loads(raw["render"])
    if MODULE.digest(raw["render"]) != current["rendered_config_sha256"]:
        raise MODULE.Failure("actual rendered producer binding")
    images = {item["Id"]: item for item in map(json.loads, raw["images"].splitlines())}
    postgres = json.loads(raw["postgres_image"])
    postgres["Config"] = json.loads(raw["postgres_config"])
    images[postgres["Id"]] = postgres
    services = current["services"]
    if len(services) != 8 or len(current["inactive_services"]) != 4:
        raise MODULE.Failure("complete actual twelve-role scope")
    MODULE.plan(services, current["project"])
    if set(render["services"]) != set(services) | set(current["inactive_services"]):
        raise MODULE.Failure("actual rendered service set")
    for name, expected in services.items():
        MODULE.rendered_guard(render["services"][name], expected, images[expected["image"]], render)
    for name in current["inactive_services"]:
        if render["services"][name].get("profiles") != ["maintenance"]:
            raise MODULE.Failure("actual inactive maintenance role")
    donor = json.loads(raw["d_constructor"])[0]
    if donor["Image"] != "sha256:c7776d61291b599bb4dd2f30eb6e7f33f2336e3b88ae800cb1d580e01661467e":
        raise MODULE.Failure("actual D cached constructor identity")
    MODULE.maintenance_isolation(donor["HostConfig"])
    changed = json.loads(json.dumps(donor["HostConfig"]))
    changed["Tmpfs"]["/tmp"] = "rw,noexec,nosuid,size=1g,mode=1777"
    try:
        MODULE.maintenance_isolation(changed)
    except MODULE.Failure:
        pass
    else:
        raise MODULE.Failure("observed D tmpfs mismatch accepted")
    print("ACTUAL_INPUT_RESULT active8 inactive4 D_observed_isolation=true pass=true")


class RunnerTests(unittest.TestCase):
    def make_runner(self, parent):
        return MODULE.Runner(Path(parent) / "receipts", active=5, cleanup=1)

    def test_actual_stdout_stderr_exit_and_physical_eof(self):
        with tempfile.TemporaryDirectory() as root:
            runner = self.make_runner(root)
            raw = runner.call([sys.executable, "-c", "import sys;print('ok');print('err',file=sys.stderr)"])
            self.assertEqual(raw, b"ok\n")
            self.assertEqual(runner.records[0]["raw"][1], b"err\n")
            self.assertTrue(runner.records[0]["released"])
            self.assertFalse(runner.pending)
            self.assertEqual(runner.finish(), 0)

    def native_plan(self):
        service = {"image": "sha256:" + "a" * 64, "cpus": "0.3", "mem_limit": 805306368,
                   "memswap_limit": 805306368, "pids_limit": 128, "user": "10001:10001",
                   "entrypoint": ["/usr/local/bin/zns"], "command": ["app"],
                   "environment": {"ZNS_CONFIG_FILE": "/config/runtime.yaml"},
                   "read_only": True, "cap_drop": ["ALL"], "security_opt": ["no-new-privileges:true"],
                   "restart": "unless-stopped", "networks": {"database": {}},
                   "volumes": [{"type": "bind", "source": "/approved/runtime.yaml", "target": "/config/runtime.yaml",
                                "read_only": True, "bind": {"create_host_path": False}}]}
        invariants = {"Config.User": "10001:10001", "Config.Entrypoint": ["/usr/local/bin/zns"],
                      "Config.Cmd": ["app"], "Config.Env": ["PATH=/usr/bin", "ZNS_CONFIG_FILE=/config/runtime.yaml"],
                      "HostConfig.NanoCpus": 300000000, "HostConfig.Memory": 805306368,
                      "HostConfig.MemorySwap": 805306368, "HostConfig.PidsLimit": 128,
                      "HostConfig.Privileged": False, "HostConfig.ReadonlyRootfs": True,
                      "HostConfig.CapAdd": None, "HostConfig.CapDrop": ["ALL"],
                      "HostConfig.SecurityOpt": ["no-new-privileges:true"],
                      "HostConfig.RestartPolicy": {"Name": "unless-stopped", "MaximumRetryCount": 0},
                      "Mounts": [{"Type": "bind", "Source": "/approved/runtime.yaml", "Destination": "/config/runtime.yaml", "RW": False}]}
        expected = {"image": service["image"], "rendered": service, "invariants": invariants,
                    "network_contract": {"mode": "owned", "complete_owned_names": ["synthetic-qa-test_database"],
                                         "native_primary_allowed_names": ["synthetic-qa-test_database"], "published_ports": {}}}
        image = {"Id": service["image"], "Config": {"Env": ["PATH=/usr/bin"], "Volumes": {}}}
        config = {"networks": {"database": {"name": "synthetic-qa-test_database"}}, "volumes": {}}
        return service, expected, image, config

    def test_current_render_cannot_create_privileged_oversized_or_wrong_mount_constructor(self):
        for change in ("privileged", "memory", "swap", "cpu", "pids", "cap_add", "cap_drop",
                       "security_opt", "read_only", "entrypoint", "command", "source", "mount-rw",
                       "mount-extra", "mount-duplicate", "mount-create", "tmpfs", "group_add",
                       "devices", "volumes_from", "pid", "ipc", "uts", "ports", "environment"):
            with self.subTest(change=change):
                service, expected, image, config = self.native_plan()
                MODULE.rendered_guard(service, expected, image, config)
                changed = json.loads(json.dumps(service))
                if change == "privileged":
                    changed["privileged"] = True
                elif change == "memory":
                    changed["mem_limit"] += 1
                elif change == "source":
                    changed["volumes"][0]["source"] = "/foreign/runtime.yaml"
                elif change == "mount-rw":
                    changed["volumes"][0]["read_only"] = False
                elif change in ("mount-extra", "mount-duplicate"):
                    changed["volumes"].append(dict(changed["volumes"][0],
                                                   target="/extra" if change == "mount-extra" else "/config/runtime.yaml"))
                elif change == "mount-create":
                    changed["volumes"][0]["bind"]["create_host_path"] = True
                else:
                    key, value = {
                        "swap": ("memswap_limit", 805306369), "cpu": ("cpus", "0.4"),
                        "pids": ("pids_limit", 129), "cap_add": ("cap_add", ["SYS_ADMIN"]),
                        "cap_drop": ("cap_drop", []), "security_opt": ("security_opt", []),
                        "read_only": ("read_only", False), "entrypoint": ("entrypoint", ["/bin/sh"]),
                        "command": ("command", ["app", "--foreign"]), "tmpfs": ("tmpfs", ["/tmp:rw,size=1g"]),
                        "group_add": ("group_add", ["0"]), "devices": ("devices", ["/dev/null"]),
                        "volumes_from": ("volumes_from", ["foreign"]), "pid": ("pid", "host"),
                        "ipc": ("ipc", "host"), "uts": ("uts", "host"),
                        "ports": ("ports", [{"host_ip": "0.0.0.0", "target": 80, "published": "8080"}]),
                        "environment": ("environment", {"ZNS_CONFIG_FILE": "/foreign/runtime.yaml"}),
                    }[change]
                    changed[key] = value
                expected["rendered"] = changed  # Even a genuine hash-bound bad render must fail.
                with self.assertRaises(MODULE.Failure):
                    MODULE.rendered_guard(changed, expected, image, config)
        service, expected, image, config = self.native_plan()
        service["mem_limit"] = str(service["mem_limit"])
        service["memswap_limit"] = str(service["memswap_limit"])
        service.pop("networks")
        service["network_mode"] = "none"
        expected["network_contract"] = {"mode": "none", "complete_owned_names": [], "native_primary_allowed_names": [], "published_ports": {}}
        MODULE.rendered_guard(service, expected, image, config)
        service["network_mode"] = "host"
        with self.assertRaises(MODULE.Failure):
            MODULE.rendered_guard(service, expected, image, config)
        for value in (False, 1.0, "1.5", "-1"):
            with self.assertRaises(MODULE.Failure):
                MODULE.rendered_bytes(value)

    def test_existing_network_and_volume_prevent_actual_create_dispatch(self):
        for kind in ("network", "volume"):
            project = "synthetic-qa-test"
            expected = {"name": project + "_owned"}
            runner = mock.Mock(pending=[])
            runner.docker.side_effect = lambda *args, **kw: (expected["name"] + "\n").encode() if args[0] == kind else b""
            services = {"app": {"name": project + "-app-1"}}
            with mock.patch.object(MODULE, "compose_config", return_value=(project, ["compose"], services, [(kind, expected)])):
                with self.assertRaises(MODULE.Failure):
                    MODULE.preflight(runner, {"owner": "owner"})
            self.assertFalse(any(call.args[0] == "compose" for call in runner.docker.call_args_list))

    def test_failed_create_cannot_remove_foreign_network_or_volume(self):
        for kind in ("network", "volume"):
            project = "synthetic-qa-test"
            expected = {"name": project + "_owned", "driver": "bridge" if kind == "network" else "local", "options": None, "internal": True}
            calls = []

            class Peer:
                pending = []
                work_end = MODULE.time.monotonic() + 5
                work_utc_end = MODULE.time.time() + 5
                unresolved_resources = False
                create_compose_pin = "0" * 64
                delivery_sources = set()

                def save(self, name, raw, *, cleanup=False):
                    pass

                def fault(self, reason):
                    pass

                def docker(self, *args, **kwargs):
                    calls.append(args)
                    if args[0] == "compose":
                        raise MODULE.Failure("synthetic partial CREATE")
                    if args[:2] == (kind, "ls"):
                        return (expected["name"] + "\n").encode() if kwargs.get("cleanup") else b""
                    if args[:2] == (kind, "inspect"):
                        return json.dumps([{"Name": expected["name"], "Driver": expected["driver"], "Labels": {"synthetic.owner": "foreign"}}]).encode()
                    return b""

            runner = Peer()
            services = {"app": {"name": project + "-app-1"}}
            with mock.patch.object(MODULE, "compose_config", return_value=(project, ["compose"], services, [(kind, expected)])), \
                    mock.patch.object(MODULE, "read_pinned", return_value={}), \
                    mock.patch.object(MODULE, "delivery_snapshot", return_value=[]):
                with self.assertRaises(MODULE.Failure):
                    MODULE.preflight(runner, {"owner": "owner"})
            self.assertTrue(runner.unresolved_resources)
            self.assertFalse(any(args[:2] == (kind, "rm") for args in calls))

    def created_fixture(self, project, service, identity):
        _, expected, _, _ = self.native_plan()
        expected["name"] = project + "-" + service + "-1"
        expected["network_contract"] = {"mode": "none", "complete_owned_names": [],
                                        "native_primary_allowed_names": [], "published_ports": {}}
        profile = {"Id": identity, "Image": expected["image"], "Name": "/" + expected["name"],
                   "State": {"Status": "created", "Pid": 0, "Running": False},
                   "NetworkSettings": {"Networks": {}}}
        for key, value in expected["invariants"].items():
            parent = profile
            parts = key.split(".")
            for part in parts[:-1]:
                parent = parent.setdefault(part, {})
            parent[parts[-1]] = value
        profile["Config"]["Labels"] = {"synthetic.owner": "owner", "com.docker.compose.project": project,
                                        "com.docker.compose.service": service}
        profile["HostConfig"]["NetworkMode"] = "none"
        return expected, profile

    def test_partial_unknown_and_failed_create_discover_whole_owned_cohort(self):
        for response in ("partial", "unknown", "failed"):
            with self.subTest(response=response), tempfile.TemporaryDirectory() as root:
                runner = self.make_runner(root)
                runner.create_compose_pin = "0" * 64
                runner.delivery_sources = set()
                project = "synthetic-qa-test"
                services, profiles = {}, {}
                for service, identity in (("app", "a" * 64), ("owner", "b" * 64)):
                    expected, profile = self.created_fixture(project, service, identity)
                    services[service] = expected
                    profiles[identity] = profile
                present, calls = set(), []

                def native(*args, **kwargs):
                    calls.append((args, kwargs))
                    if args[0] == "compose":
                        self.assertEqual(runner.owned, ["app", "owner"])
                        self.assertTrue(runner.unresolved_resources)
                        present.update(profiles)
                        if response != "partial":
                            raise MODULE.Failure("unknown client response" if response == "unknown" else "CREATE exit9")
                        return b"ignored partial client stdout"
                    if args[0] == "inspect":
                        return json.dumps([profiles[args[1]]]).encode()
                    if args[0] == "rm":
                        self.assertEqual(len(args), 2)
                        self.assertIn(args[1], present)
                        present.remove(args[1])
                        return b""
                    selector = args[args.index("--filter") + 1]
                    if selector.startswith("label="):
                        ids = sorted(present) if kwargs.get("cleanup") else sorted(present)[:1]
                    elif selector.startswith("id="):
                        ids = [selector[3:]] if selector[3:] in present else []
                    else:
                        ids = [identity for identity in present if selector == "name=^" + profiles[identity]["Name"] + "$" ]
                    return "\n".join(ids).encode()

                runner.docker = native
                with mock.patch.object(MODULE, "compose_config", return_value=(project, ["compose"], services, [])), \
                        mock.patch.object(MODULE, "read_pinned", return_value={}), \
                        mock.patch.object(MODULE, "delivery_snapshot", return_value=[]):
                    with self.assertRaises(MODULE.Failure):
                        MODULE.preflight(runner, {"owner": "owner"})
                self.assertFalse(present)
                self.assertFalse(runner.unresolved_resources)
                self.assertEqual([args[1] for args, _ in calls if args[0] == "rm"], ["a" * 64, "b" * 64])
                self.assertTrue(any("--no-trunc" in args and "id=" + "a" * 64 in args for args, _ in calls))
                self.assertTrue(any("name=^/synthetic\\-qa\\-test\\-app\\-1$" in args for args, _ in calls))
                self.assertEqual(runner.finish(), 1)

    def test_create_cleanup_refuses_foreign_or_started_containers_and_uncertain_absence(self):
        for fault in ("foreign", "started", "discovery", "empty-discovery", "prefix-discovery", "inspect-mismatch",
                      "full-id-query", "name-query", "full-id-present", "name-present"):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as root:
                runner = self.make_runner(root)
                runner.create_compose_pin = "0" * 64
                runner.delivery_sources = set()
                project, identity = "synthetic-qa-test", "a" * 64
                expected, profile = self.created_fixture(project, "app", identity)
                if fault == "foreign":
                    profile["Config"]["Labels"]["synthetic.owner"] = "foreign"
                elif fault == "started":
                    profile["State"] = {"Status": "running", "Running": True, "Pid": 42}
                elif fault == "inspect-mismatch":
                    profile["Id"] = "b" * 64
                calls = []

                def native(*args, **kwargs):
                    calls.append(args)
                    if args[0] == "compose":
                        raise MODULE.Failure("first CREATE fault")
                    if args[0] == "inspect":
                        return json.dumps([profile]).encode()
                    if args[0] == "rm":
                        self.assertEqual(args, ("rm", identity))
                        return b""
                    if not kwargs.get("cleanup"):
                        return b""
                    selector = args[args.index("--filter") + 1]
                    if selector.startswith("label="):
                        if fault == "discovery":
                            raise MODULE.Failure("discovery unavailable")
                        if fault == "empty-discovery":
                            return b""
                        if fault == "prefix-discovery":
                            return identity[:12].encode()
                        return identity.encode()
                    if selector == "id=" + identity:
                        if fault == "full-id-query":
                            raise MODULE.Failure("ID query unavailable")
                        return identity.encode() if fault == "full-id-present" else b""
                    if fault == "name-query":
                        raise MODULE.Failure("name query unavailable")
                    return identity.encode() if fault in ("name-present", "empty-discovery") else b""

                runner.docker = native
                with mock.patch.object(MODULE, "compose_config", return_value=(project, ["compose"], {"app": expected}, [])), \
                        mock.patch.object(MODULE, "read_pinned", return_value={}), \
                        mock.patch.object(MODULE, "delivery_snapshot", return_value=[]) as delivery:
                    with self.assertRaises(MODULE.Failure):
                        MODULE.preflight(runner, {"owner": "owner"})
                self.assertTrue(runner.unresolved_resources)
                self.assertEqual(runner.first, "first CREATE fault")
                self.assertEqual(delivery.call_count, 1)
                if fault in ("foreign", "started", "discovery", "empty-discovery", "prefix-discovery", "inspect-mismatch"):
                    self.assertFalse(any(args[0] == "rm" for args in calls))

    def test_existing_or_symlinked_output_never_overwrites_owned_inputs(self):
        with tempfile.TemporaryDirectory() as root:
            source = Path(root) / "source"
            source.mkdir()
            member = source / "input.json"
            member.write_bytes(b"unchanged input")
            alias = Path(root) / "alias"
            alias.symlink_to(source, target_is_directory=True)
            for output in (source, alias):
                with self.assertRaises(FileExistsError):
                    MODULE.Runner(output)
            self.assertEqual(member.read_bytes(), b"unchanged input")
            self.assertEqual(list(source.iterdir()), [member])

    def test_physical_links_and_output_overlap_fail_delivery_admission(self):
        for fault in ("file-symlink", "directory-symlink", "hardlink", "overlap", "manifest-mismatch"):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as root:
                runner = self.make_runner(root)
                source = Path(root) / "source"
                source.mkdir()
                member = source / "config.json"
                member.write_bytes(b"actual admitted bytes")
                approval = {"no_host_source_writers": True,
                            "delivery_files": [{"path": str(member), "bytes": member.stat().st_size,
                                                "sha256": MODULE.digest(member.read_bytes())}],
                            "delivery_directories": [{"path": str(source), "members": ["config.json"], "directories": []}]}
                if fault == "file-symlink":
                    target = Path(root) / "target.json"
                    member.rename(target)
                    member.symlink_to(target)
                elif fault == "directory-symlink":
                    target = Path(root) / "target"
                    source.rename(target)
                    source.symlink_to(target, target_is_directory=True)
                elif fault == "hardlink":
                    os.link(member, Path(root) / "duplicate.json")
                elif fault == "overlap":
                    with self.assertRaises(MODULE.Failure):
                        MODULE.Runner(source / "operator-output", sources=[source])
                    self.assertFalse((source / "operator-output").exists())
                    self.assertEqual(list(source.iterdir()), [member])
                    continue
                else:
                    approval["delivery_files"][0]["sha256"] = "0" * 64
                with mock.patch.object(MODULE, "DAEMON_SOURCE_PREFIX", root + "/"):
                    with self.assertRaises((MODULE.Failure, OSError)):
                        MODULE.delivery_snapshot(runner, approval, {str(source)})

    def test_actual_delivered_hash_precedes_main_and_image_missing_precedes_create(self):
        with tempfile.TemporaryDirectory() as root:
            compose = Path(root) / "compose.yaml"
            compose.write_bytes(b"synthetic render input")
            peer = mock.Mock()
            peer.work_end, peer.work_utc_end = MODULE.time.monotonic() + 5, MODULE.time.time() + 5
            peer.docker.side_effect = [b"approved-daemon\n", b"{}"]
            MODULE.compose_config(peer, {"project": "synthetic-qa-test", "operation": "C_RENDER",
                                        "daemon_id": "approved-daemon", "compose_file": str(compose),
                                        "compose_sha256": MODULE.digest(compose.read_bytes())})
            self.assertEqual(peer.docker.call_args.args[-5:],
                             ("--profile", "maintenance", "config", "--format", "json"))
        for substitution in ("none", "hash", "main", "import", "guard-import", "unadmitted", "bytecode",
                             "main-error", "import-error", "compile-error", "main-error-drift",
                             "import-error-drift", "compile-error-drift", "exit-none", "exit-zero",
                             "exit-false", "exit-string", "exit-float", "exit-list", "exit-string-drift"):
            with self.subTest(substitution=substitution), tempfile.TemporaryDirectory() as root:
                local, transport = Path(root) / "input", Path(root) / "transport"
                local.mkdir()
                transport.mkdir()
                main, module = local / "test-maintenance.py", local / "run-maintenance.py"
                qualified, guards = transport / "run-qualified.py", transport / "guards.py"
                main_raw = ("import importlib.util,sys\n"
                            "assert __name__=='__main__' and sys.modules[__name__].__file__==__file__\n"
                            "def load(name,path):\n"
                            " spec=importlib.util.spec_from_file_location(name,path)\n"
                            " obj=importlib.util.module_from_spec(spec);spec.loader.exec_module(obj);return obj\n"
                            "value=load('admitted_local'," + repr(str(module)) + ").VALUE\n"
                            "assert value=='AUTHENTICATED_IMPORT'\n"
                            "assert load('admitted_transport'," + repr(str(qualified)) + ").VALUE=='AUTHENTICATED_TRANSPORT'\n"
                            "print('AUTHENTICATED_MAIN')\n").encode()
                module_raw = b"VALUE='AUTHENTICATED_IMPORT'\n"
                if substitution.startswith("main-error"):
                    main_raw += b"raise RuntimeError('original main failure')\n"
                elif substitution.startswith("import-error"):
                    module_raw = b"raise ImportError('original import failure')\n"
                exit_payloads = {"none": None, "zero": 0, "false": False, "string": "", "float": 0.0, "list": []}
                if substitution.startswith("exit-"):
                    exit_payload = exit_payloads[substitution.split("-")[1]]
                    main_raw = ("raise SystemExit(" + repr(exit_payload) + ")\n").encode()
                if substitution == "unadmitted":
                    extra = local / "extra.py"
                    extra.write_bytes(b"raise AssertionError('unadmitted body executed')\n")
                    module_raw = ("import importlib.util\n"
                                  "spec=importlib.util.spec_from_file_location('extra'," + repr(str(extra)) + ")\n"
                                  "obj=importlib.util.module_from_spec(spec);spec.loader.exec_module(obj)\n").encode()
                elif substitution == "bytecode":
                    import py_compile
                    extra = local / "extra.py"
                    extra.write_bytes(b"raise AssertionError('unadmitted bytecode executed')\n")
                    bytecode = local / "extra.pyc"
                    py_compile.compile(str(extra), cfile=str(bytecode), doraise=True)
                    extra.unlink()
                    module_raw = ("import importlib.util\n"
                                  "spec=importlib.util.spec_from_file_location('extra'," + repr(str(bytecode)) + ")\n"
                                  "obj=importlib.util.module_from_spec(spec);spec.loader.exec_module(obj)\n").encode()
                original = {main: main_raw, module: module_raw,
                            qualified: ("import sys\nsys.path.insert(0," + repr(str(transport)) + ")\n"
                                        "try:\n import guards\n VALUE=guards.VALUE\n"
                                        "finally:\n sys.path.pop(0)\n").encode(),
                            guards: b"VALUE='AUTHENTICATED_TRANSPORT'\n"}
                for path, raw in original.items():
                    path.write_bytes(raw)
                manifest = {str(path): MODULE.digest(raw) for path, raw in original.items()}
                if substitution == "hash":
                    manifest[str(main)] = "0" * 64
                code = MODULE.D_SOURCE_VERIFY.replace("/input", str(local)).replace("/transport", str(transport))
                real_compile = compile
                compiled, replaced = [], []
                loader = importlib.machinery.SourceFileLoader
                previous_read, previous_main = loader.get_data, sys.modules.get("__main__")

                def capture(raw, filename, *args, **kwargs):
                    if filename == str(main):
                        compiled.append(raw)
                        if substitution.endswith("-drift"):
                            module.write_bytes(b"raise AssertionError('drift body executed')\n")
                        if substitution.startswith("compile-error"):
                            raise SyntaxError("original compile failure")
                        if substitution in ("main", "import", "guard-import"):
                            target = {"main": main, "import": module, "guard-import": guards}[substitution]
                            target.write_bytes(b"raise AssertionError('substituted bytes executed')\n")
                            replaced.append(target)
                    result = real_compile(raw, filename, *args, **kwargs)
                    if filename == str(guards):
                        for target in replaced:
                            target.write_bytes(original[target])
                    return result

                with mock.patch.object(sys, "argv", ["verify", json.dumps(manifest)]), \
                        mock.patch("builtins.compile", side_effect=capture), \
                        mock.patch.object(os, "open", wraps=os.open) as opened, \
                        mock.patch.dict(sys.modules), \
                        contextlib.redirect_stdout(io.StringIO()) as output:
                    if substitution in ("exit-string", "exit-float", "exit-list", "exit-string-drift"):
                        with self.assertRaises(SystemExit) as caught:
                            exec(code, {})
                        self.assertEqual(caught.exception.code, exit_payload)
                        self.assertIs(type(caught.exception.code), type(exit_payload))
                        if substitution.endswith("-drift"):
                            self.assertIsInstance(caught.exception.__cause__, RuntimeError)
                            self.assertIn("source hash mismatch", str(caught.exception.__cause__))
                        else:
                            self.assertIsNone(caught.exception.__cause__)
                            for path in original:
                                self.assertEqual(sum(call.args[0] == str(path) for call in opened.call_args_list), 2)
                        self.assertNotIn("D001_DELIVERED_SOURCE_PROOF ", output.getvalue())
                    elif "-error" in substitution:
                        kind = substitution.split("-")[0]
                        failure = {"main": RuntimeError, "import": ImportError, "compile": SyntaxError}[kind]
                        with self.assertRaisesRegex(failure, "original " + kind + " failure") as caught:
                            exec(code, {})
                        if substitution.endswith("-drift"):
                            self.assertIsInstance(caught.exception.__cause__, RuntimeError)
                            self.assertIn("source hash mismatch", str(caught.exception.__cause__))
                        else:
                            self.assertIsNone(caught.exception.__cause__)
                            for path in original:
                                self.assertEqual(sum(call.args[0] == str(path) for call in opened.call_args_list), 2)
                        self.assertNotIn("D001_DELIVERED_SOURCE_PROOF ", output.getvalue())
                    elif substitution in ("hash", "unadmitted", "bytecode"):
                        expected = "source hash" if substitution == "hash" else "not admitted"
                        with self.assertRaisesRegex((RuntimeError, FileNotFoundError), expected):
                            exec(code, {})
                    else:
                        exec(code, {})
                        if not substitution.startswith("exit-"):
                            self.assertIn("AUTHENTICATED_MAIN", output.getvalue())
                        self.assertIn("D001_DELIVERED_SOURCE_PROOF ", output.getvalue())
                        self.assertEqual(compiled, [main_raw])
                self.assertIs(loader.get_data, previous_read)
                self.assertIs(sys.modules.get("__main__"), previous_main)
                if substitution == "hash":
                    self.assertEqual(compiled, [])
        peer = mock.Mock()
        peer.docker.side_effect = [b"approved-daemon\n", json.dumps([{"Id": "sha256:" + "0" * 64}]).encode()]
        with self.assertRaises(MODULE.Failure):
            MODULE.maintenance_controls(peer, {"project": "synthetic-qa-test", "daemon_id": "approved-daemon"})
        self.assertFalse(any(call.args[0] == "create" for call in peer.docker.call_args_list))
        with tempfile.TemporaryDirectory() as root:
            inputs = []
            for index, target in enumerate(("/input/run-maintenance.py", "/input/test-maintenance.py",
                                             "/transport/run-qualified.py", "/transport/guards.py")):
                path = Path(root) / str(index)
                path.write_bytes(b"synthetic public source")
                inputs.append({"target": target, "operator_path": str(path), "sha256": MODULE.digest(path.read_bytes()),
                               "daemon_source": "/run/desktop/mnt/host/c/Users/ddriz/Projects/zns-chatbot/qa.local/synthetic/" + str(index)})
            peer = mock.Mock(pending=[])
            peer.work_end, peer.work_utc_end = MODULE.time.monotonic() + 5, MODULE.time.time() + 5

            def reply(*args, **kwargs):
                if args[0] == "info":
                    return b"approved-daemon\n"
                if args[:2] == ("image", "inspect"):
                    return json.dumps([{"Id": "sha256:c7776d61291b599bb4dd2f30eb6e7f33f2336e3b88ae800cb1d580e01661467e"}]).encode()
                if args[0] == "create":
                    self.assertEqual(args[1:3], ("--pull", "never"))
                    raise MODULE.Failure("synthetic prebirth stop")
                return b""

            peer.docker.side_effect = reply
            with self.assertRaises(MODULE.Failure):
                MODULE.maintenance_controls(peer, {"project": "synthetic-qa-test", "daemon_id": "approved-daemon", "inputs": inputs})
            self.assertTrue(any(call.args[0] == "create" for call in peer.docker.call_args_list))

    def test_native_exit_precedes_both_independent_storage_faults(self):
        for failed in ({"001.stdout"}, {"001.stderr"}, {"001.stdout", "001.stderr"}, {"terminal.json"}):
            with self.subTest(failed=failed), tempfile.TemporaryDirectory() as root:
                runner = self.make_runner(root)
                original = runner.save
                secret = "private-token-synthetic-do-not-publish"

                def broken(name, raw, **kwargs):
                    if name in failed:
                        raise OSError(secret)
                    original(name, raw, **kwargs)

                runner.save = broken
                with self.assertRaises(MODULE.Failure):
                    runner.call([sys.executable, "-c", "import sys;print('out');print('err',file=sys.stderr);sys.exit(9)"])
                record = runner.records[0]
                self.assertEqual(record["first_fault"], "unexpected native exit")
                self.assertEqual(record["raw"], (b"out\n", b"err\n"))
                self.assertEqual(record["exit"], 9)
                self.assertEqual(record["later_faults"].count("native receipt storage fault"),
                                 len(failed - {"terminal.json"}))
                self.assertTrue(record["released"])
                self.assertTrue(record["reaped"])
                self.assertEqual(record["eof"], [True, True])
                self.assertEqual(runner.finish(), 1)
                self.assertNotIn(secret, json.dumps({key: value for key, value in record.items() if key != "raw"}))
                self.assertNotIn(secret, str(runner.first) + str(runner.later_faults))
                for name, raw in (("001.stdout", b"out\n"), ("001.stderr", b"err\n")):
                    self.assertEqual((runner.output / name).exists(), name not in failed)
                    if name not in failed:
                        self.assertEqual((runner.output / name).read_bytes(), raw)
                if "terminal.json" in failed:
                    self.assertIn("terminal storage fault", runner.later_faults)
                else:
                    terminal = json.loads((runner.output / "terminal.json").read_bytes())
                    self.assertIsNone(terminal["pass"])
                    self.assertEqual(terminal["failure"], "unexpected native exit")
                    self.assertNotIn(secret, json.dumps(terminal))

    def test_overflow_is_task_failure_not_unresolved_physical_custody(self):
        with tempfile.TemporaryDirectory() as root:
            runner = self.make_runner(root)
            with self.assertRaises(MODULE.Failure):
                runner.call([sys.executable, "-c", "import sys;sys.stdout.write('x'*140000)"])
            record = runner.records[0]
            self.assertEqual(sum(map(len, record["raw"])), 131072)
            self.assertTrue(record["released"])
            self.assertFalse(runner.pending)
            self.assertEqual(runner.finish(), 1)

    def test_bool_and_numeric_substitutions_rejected(self):
        self.assertFalse(MODULE.same({"pid": 0}, {"pid": False}))
        self.assertFalse(MODULE.same({"cpu": 1}, {"cpu": 1.0}))
        self.assertTrue(MODULE.same({"mounts": [{"rw": False}]}, {"mounts": [{"rw": False}]}))
        self.assertEqual(MODULE.environment(["A=1", "B=2"]), MODULE.environment(["B=2", "A=1"]))
        with self.assertRaises(MODULE.Failure):
            MODULE.environment(["A=1", "A=1"])
        semantic = {"Type": "volume", "Name": "owned", "Destination": "/data", "RW": True}
        native = dict(semantic, Source="/engine/volume/_data", Driver="local", Mode="z", Propagation="")
        self.assertEqual(MODULE.mount_invariants([semantic]), MODULE.mount_invariants([native]))
        service, expected, image, config = self.native_plan()
        service["networks"]["helpers"] = {}
        config["networks"]["helpers"] = {"name": "synthetic-qa-test_helpers"}
        expected["network_contract"] = {"mode": "owned", "complete_owned_names": ["synthetic-qa-test_database", "synthetic-qa-test_helpers"],
                                         "native_primary_allowed_names": ["synthetic-qa-test_database", "synthetic-qa-test_helpers"], "published_ports": {}}
        MODULE.rendered_guard(service, expected, image, config)
        expected["name"] = "synthetic-qa-test-app-1"
        profile = {"Image": expected["image"], "Name": "/" + expected["name"]}
        for key, value in expected["invariants"].items():
            parent = profile
            parts = key.split(".")
            for part in parts[:-1]:
                parent = parent.setdefault(part, {})
            parent[parts[-1]] = value
        profile["Config"]["Labels"] = {"synthetic.owner": "owner", "com.docker.compose.project": "synthetic-qa-test",
                                        "com.docker.compose.service": "app"}
        profile["HostConfig"]["NetworkMode"] = "synthetic-qa-test_helpers"
        profile["NetworkSettings"] = {"Networks": {name: {} for name in expected["network_contract"]["complete_owned_names"]}}
        MODULE.constructor(profile, expected, "synthetic-qa-test", "app", "owner")
        for dotted, value in (("HostConfig.MemorySwap", 805306369), ("HostConfig.CapAdd", ["SYS_ADMIN"]),
                              ("HostConfig.CapDrop", []), ("HostConfig.ReadonlyRootfs", False),
                              ("HostConfig.Privileged", True), ("HostConfig.SecurityOpt", []),
                              ("Config.Cmd", ["app", "--foreign"]), ("Config.Entrypoint", ["/bin/sh"]),
                              ("HostConfig.PortBindings", {"80/tcp": [{"HostIp": "0.0.0.0", "HostPort": "80"}]})):
            changed = json.loads(json.dumps(profile))
            parent, field = dotted.split(".")
            changed[parent][field] = value
            with self.subTest(native=dotted), self.assertRaises(MODULE.Failure):
                MODULE.constructor(changed, expected, "synthetic-qa-test", "app", "owner")
        helper = {"NanoCpus": 200000000, "Memory": 268435456, "MemorySwap": 268435456,
                  "PidsLimit": 64, "NetworkMode": "none", "ReadonlyRootfs": True,
                  "Privileged": False, "CapAdd": None, "CapDrop": ["ALL"],
                  "SecurityOpt": ["no-new-privileges"],
                  "RestartPolicy": {"Name": "no", "MaximumRetryCount": 0},
                  "Tmpfs": {"/tmp": "rw,noexec,nosuid,nodev,size=16m,mode=1777"}}
        MODULE.maintenance_isolation(helper)
        for key, value in (("MemorySwap", 536870912), ("NanoCpus", 300000000), ("Memory", 536870912),
                           ("PidsLimit", 65), ("NetworkMode", "host"), ("ReadonlyRootfs", False),
                           ("Privileged", True), ("CapAdd", ["SYS_ADMIN"]), ("CapDrop", []),
                           ("SecurityOpt", []), ("RestartPolicy", {"Name": "always", "MaximumRetryCount": 0}),
                           ("Tmpfs", {"/tmp": "rw,noexec,nosuid,size=16m,mode=1777"})):
            changed = dict(helper, **{key: value})
            with self.subTest(helper=key), self.assertRaises(MODULE.Failure):
                MODULE.maintenance_isolation(changed)
        profile["NetworkSettings"]["Networks"]["foreign"] = {}
        with self.assertRaises(MODULE.Failure):
            MODULE.constructor(profile, expected, "synthetic-qa-test", "app", "owner")
        expected["network_contract"]["native_primary_allowed_names"] = ["foreign"]
        with self.assertRaises(MODULE.Failure):
            MODULE.rendered_guard(service, expected, image, config)
        service, expected, image, config = self.native_plan()
        expected["name"] = "synthetic-qa-test-app-1"
        expected["invariants"]["Mounts"] = [semantic]
        MODULE.plan({"app": expected}, "synthetic-qa-test")
        expected["invariants"]["Mounts"][0]["Name"] = "docker.sock"
        with self.assertRaises(MODULE.Failure):
            MODULE.plan({"app": expected}, "synthetic-qa-test")
        service, expected, image, config = self.native_plan()
        service["entrypoint"] = None
        service["command"] = None
        image["Config"]["Entrypoint"] = ["/inherited-entry"]
        image["Config"]["Cmd"] = ["inherited-command"]
        expected["invariants"]["Config.Entrypoint"] = ["/inherited-entry"]
        expected["invariants"]["Config.Cmd"] = ["inherited-command"]
        MODULE.rendered_guard(service, expected, image, config)
        service["entrypoint"] = []
        service["command"] = []
        expected["invariants"]["Config.Entrypoint"] = []
        expected["invariants"]["Config.Cmd"] = []
        MODULE.rendered_guard(service, expected, image, config)
        service.pop("user")
        expected["invariants"]["Config.User"] = ""
        MODULE.rendered_guard(service, expected, image, config)
        expected["invariants"]["Config.User"] = None
        with self.assertRaises(MODULE.Failure):
            MODULE.rendered_guard(service, expected, image, config)

    def test_utc_expiry_prevents_birth(self):
        for duration in (None, float("nan"), float("inf"), float("-inf"), 0, -1, True, "1"):
            with self.subTest(duration=duration), tempfile.TemporaryDirectory() as root:
                runner = self.make_runner(root)
                with mock.patch.object(MODULE.subprocess.Popen, "__init__") as child, \
                        self.assertRaisesRegex(MODULE.Failure, "finite positive native duration"):
                    runner.call([sys.executable, "-c", "raise AssertionError('not run')"], seconds=duration)
                child.assert_not_called()
                self.assertEqual(runner.records, [])
                self.assertFalse(runner.pending)
                self.assertEqual(list(runner.output.iterdir()), [])
        with tempfile.TemporaryDirectory() as root:
            runner = self.make_runner(root)
            runner.work_utc_end = 0
            with self.assertRaises(MODULE.Failure):
                runner.call([sys.executable, "-c", "raise AssertionError('not run')"])
            self.assertEqual(runner.records, [])

    def test_inherited_writer_retains_true_eof_and_passive_host_custody(self):
        class PassiveHold(Exception):
            pass

        with tempfile.TemporaryDirectory() as root:
            runner = self.make_runner(root)
            fixture_end = MODULE.time.monotonic() + 2
            # Adopt only this fixture's orphan so its exact PID can be reaped here.
            libc = ctypes.CDLL(None, use_errno=True)
            subreaper = ctypes.c_int()
            self.assertEqual(libc.prctl(37, ctypes.byref(subreaper), 0, 0, 0), 0)
            self.assertEqual(libc.prctl(36, 1, 0, 0, 0), 0)
            release = Path(root) / "release-writer"
            ready = Path(root) / "writer-ready"
            code = """import os,sys,time
from pathlib import Path
ready,release=map(Path,sys.argv[1:])
if os.fork()==0:
    ready.write_text(str(os.getpid()))
    while not release.exists():
        time.sleep(.01)
    os.write(1,b'late-writer\\n')
    os._exit(0)
while not ready.exists():
    time.sleep(.01)
os.write(1,b'leader-done\\n')
os._exit(0)
"""
            try:
                with mock.patch.object(MODULE.os, "killpg", wraps=os.killpg) as kill:
                    with self.assertRaises(MODULE.Failure):
                        runner.call([sys.executable, "-c", code, str(ready), str(release)], seconds=1)
                    kill.assert_not_called()
                record = runner.records[0]
                self.assertEqual(record["exit"], 0)
                self.assertTrue(record["reaped"])
                self.assertEqual(record["eof"], [False, False])
                self.assertFalse(record["released"])
                self.assertEqual(record["raw"], (b"leader-done\n", b""))
                self.assertEqual(len(runner.pending), 1)
                with mock.patch.object(MODULE.subprocess, "Popen") as birth:
                    for cleanup in (False, True):
                        with self.assertRaisesRegex(MODULE.Failure, "custody forbids dispatch"):
                            runner.call([sys.executable, "-c", "pass"], cleanup=cleanup)
                    birth.assert_not_called()
                with mock.patch.object(MODULE.time, "sleep", side_effect=PassiveHold), \
                        mock.patch.object(MODULE.subprocess, "Popen") as birth, \
                        mock.patch.object(MODULE.os, "read") as read:
                    with self.assertRaises(PassiveHold):
                        runner.finish()
                    birth.assert_not_called()
                    read.assert_not_called()
                terminal = json.loads((runner.output / "terminal.json").read_bytes())
                self.assertEqual(terminal["unresolved"], [runner.pending[0].pid])
                self.assertIsNone(terminal["pass"])
                self.assertFalse(terminal["eligible_before_publication"])
            finally:
                try:
                    release.touch()
                    if runner.pending:
                        writer_pid = int(ready.read_text())
                        stdout, stderr = runner.pending[0].communicate(
                            timeout=max(0, fixture_end - MODULE.time.monotonic()))
                        self.assertEqual(stdout, b"late-writer\n")
                        self.assertEqual(stderr, b"")
                        self.assertIsNotNone(runner.pending[0].poll())
                        while MODULE.time.monotonic() < fixture_end:
                            observed, status = os.waitpid(writer_pid, os.WNOHANG)
                            if observed == writer_pid:
                                self.assertEqual(os.waitstatus_to_exitcode(status), 0)
                                break
                            MODULE.time.sleep(.005)
                        else:
                            self.fail("owned fixture writer reap unavailable within original fixture bound")
                        with self.assertRaises(ChildProcessError):
                            os.waitpid(writer_pid, os.WNOHANG)
                        # Physical test teardown is not runner recovery or acceptance.
                        self.assertFalse(runner.records[0]["released"])
                finally:
                    self.assertEqual(libc.prctl(36, subreaper.value, 0, 0, 0), 0)

    def test_closed_pipes_do_not_release_a_living_child(self):
        for boundary in ("select", "read", "completed-poll"):
            with self.subTest(boundary=boundary), tempfile.TemporaryDirectory() as root:
                runner = self.make_runner(root)
                real_clock, real_read = MODULE.time.monotonic, MODULE.os.read
                real_poll, real_selector = MODULE.subprocess.Popen.poll, MODULE.selectors.DefaultSelector
                delay = [0]

                def read(fd, size):
                    raw = real_read(fd, size)
                    if (boundary == "read" and raw == b"retained" and runner.pending
                            and fd in runner.pending[0]._qa_reader_fds):
                        delay[0] = 10
                    return raw

                def poll(process):
                    code = real_poll(process)
                    if (boundary == "completed-poll" and code == 0 and runner.records
                            and all(runner.records[0]["eof"])):
                        delay[0] = 10
                    return code

                def selector():
                    value = real_selector()
                    original = value.select

                    def select(timeout):
                        ready = original(timeout)
                        if boundary == "select" and ready:
                            delay[0] = 10
                        return ready

                    value.select = select
                    return value

                try:
                    with mock.patch.object(MODULE.time, "monotonic", side_effect=lambda: real_clock() + delay[0]), \
                            mock.patch.object(MODULE.os, "read", side_effect=read), \
                            mock.patch.object(MODULE.subprocess.Popen, "poll", poll), \
                            mock.patch.object(MODULE.selectors, "DefaultSelector", side_effect=selector):
                        with self.assertRaises(MODULE.Failure):
                            runner.call([sys.executable, "-c", "import os;os.write(1,b'retained')"], seconds=2)
                    record = runner.records[0]
                    self.assertFalse(record["released"])
                    self.assertEqual(len(runner.pending), 1)
                    if boundary in ("read", "completed-poll"):
                        self.assertEqual(record["raw"][0], b"retained")
                    if boundary == "completed-poll":
                        self.assertEqual(record["exit"], 0)
                        self.assertTrue(record["reaped"])
                        self.assertEqual(record["eof"], [True, True])
                    with mock.patch.object(MODULE.subprocess.Popen, "__init__") as child:
                        with self.assertRaisesRegex(MODULE.Failure, "custody forbids dispatch"):
                            runner.call([sys.executable, "-c", "pass"], cleanup=True)
                        child.assert_not_called()
                finally:
                    for process in runner.pending:
                        process._qa_acquiring_thread.join(timeout=1)
                        self.assertFalse(process._qa_acquiring_thread.is_alive())
                        process.wait(timeout=1)
                        for fd in process._qa_writers:
                            os.close(fd)
                        for stream in process._qa_readers:
                            stream.close()
        with tempfile.TemporaryDirectory() as root:
            runner = self.make_runner(root)
            with self.assertRaises(MODULE.Failure):
                runner.call([sys.executable, "-c", "import os,time;os.close(1);os.close(2);time.sleep(10)"], seconds=1)
            record = runner.records[0]
            self.assertEqual(record["first_fault"], "native deadline")
            self.assertEqual(record["exit"], -signal.SIGKILL)
            self.assertEqual(record["eof"], [True, True])
            self.assertTrue(record["reaped"])
            self.assertTrue(record["released"])
            self.assertFalse(runner.pending)

    def test_signal_exit_survives_later_operator_failure(self):
        with tempfile.TemporaryDirectory() as root:
            runner = self.make_runner(root)
            with self.assertRaises(MODULE.Failure):
                runner.call([sys.executable, "-c", "import os,signal;os.kill(os.getpid(),signal.SIGTERM)"])
            record = runner.records[0]
            self.assertEqual(record["exit"], -signal.SIGTERM)
            self.assertEqual(record["first_fault"], "unexpected native exit")
            self.assertTrue(record["released"])
            runner.fault("later cleanup error")
            self.assertEqual(runner.finish(), 1)
            terminal = json.loads((runner.output / "terminal.json").read_bytes())
            self.assertEqual(terminal["failure"], "unexpected native exit")
            self.assertIn("later cleanup error", terminal["later_faults"])

    def test_default_main_rejects_raw_admission_before_output_or_dispatch(self):
        valid = {"authority": "ROOT", "operation": "C_RENDER", "passive_native_custody_allowed": True}
        for fault in ("hash", "authority", "operation", "custody", "json", "size"):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as root:
                approval = dict(valid)
                if fault in ("authority", "operation"):
                    approval[fault] = "FOREIGN"
                elif fault == "custody":
                    approval["passive_native_custody_allowed"] = 1
                raw = b"{" if fault == "json" else json.dumps(approval).encode()
                if fault == "size":
                    raw = b" " * 1048577
                path = Path(root) / "approval.json"
                path.write_bytes(raw)
                output = Path(root) / "absent-output"
                pin = "0" * 64 if fault == "hash" else MODULE.digest(raw)
                argv = ["runner", "--approval", str(path), "--approval-sha256", pin, "--output", str(output)]
                with mock.patch.object(sys, "argv", argv), \
                        mock.patch.object(MODULE.subprocess, "Popen") as birth, \
                        mock.patch.object(MODULE, "compose_config") as dispatch, \
                        contextlib.redirect_stderr(io.StringIO()) as errors:
                    self.assertEqual(MODULE.main(), 1)
                    birth.assert_not_called()
                    dispatch.assert_not_called()
                self.assertFalse(output.exists())
                self.assertEqual(errors.getvalue(), "ROOT approval rejected before operator setup\n")

    def test_default_main_preserves_initial_intake_clock_before_setup(self):
        for clock in ("monotonic", "time"):
            with self.subTest(clock=clock), tempfile.TemporaryDirectory() as root:
                path = Path(root) / "approval.json"
                raw = json.dumps({"authority": "ROOT", "operation": "C_RENDER", "passive_native_custody_allowed": True}).encode()
                path.write_bytes(raw)
                output = Path(root) / "receipts"
                argv = ["runner", "--approval", str(path), "--approval-sha256", MODULE.digest(raw), "--output", str(output)]
                real_clock = getattr(MODULE.time, clock)
                read = MODULE.read_pinned
                offset = [0]

                def expired(*args):
                    value = read(*args)
                    offset[0] = 90
                    return value

                with mock.patch.object(sys, "argv", argv), \
                        mock.patch.object(MODULE, "read_pinned", side_effect=expired), \
                        mock.patch.object(MODULE.time, clock, side_effect=lambda: real_clock() + offset[0]), \
                        mock.patch.object(MODULE, "Runner") as constructor, \
                        contextlib.redirect_stderr(io.StringIO()):
                    self.assertEqual(MODULE.main(), 1)
                    constructor.assert_not_called()
                self.assertFalse(output.exists())
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / "approval.json"
            raw = json.dumps({"authority": "ROOT", "operation": "C_RENDER", "passive_native_custody_allowed": True}).encode()
            path.write_bytes(raw)
            argv = ["runner", "--approval", str(path), "--approval-sha256", MODULE.digest(raw), "--output", str(Path(root) / "receipts")]
            with mock.patch.object(sys, "argv", argv), \
                    mock.patch.object(MODULE.time, "monotonic", return_value=100), \
                    mock.patch.object(MODULE.time, "time", return_value=200), \
                    mock.patch.object(MODULE, "compose_config") as dispatch:
                self.assertEqual(MODULE.main(), 0)
                runner = dispatch.call_args.args[0]
                self.assertEqual((runner.start, runner.utc_start), (100, 200))
                self.assertEqual((runner.work_end, runner.end), (190, 220))
                self.assertEqual((runner.work_utc_end, runner.utc_end), (290, 320))

    def test_default_main_rejects_fifo_directory_and_physical_approval_aliases(self):
        for fault in ("fifo", "directory", "leaf-link", "ancestor-link", "hardlink"):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as root:
                raw = json.dumps({"authority": "ROOT", "operation": "C_RENDER", "passive_native_custody_allowed": True}).encode()
                path = Path(root) / "approval.json"
                if fault == "fifo":
                    os.mkfifo(path)
                elif fault == "directory":
                    path.mkdir()
                elif fault == "ancestor-link":
                    physical = Path(root) / "physical"
                    physical.mkdir()
                    (physical / "approval.json").write_bytes(raw)
                    (Path(root) / "alias").symlink_to(physical, target_is_directory=True)
                    path = Path(root) / "alias/approval.json"
                else:
                    path.write_bytes(raw)
                    alias = Path(root) / "alias.json"
                    if fault == "leaf-link":
                        alias.symlink_to(path)
                    else:
                        os.link(path, alias)
                    path = alias
                output = Path(root) / "absent-output"
                argv = ["runner", "--approval", str(path), "--approval-sha256", MODULE.digest(raw), "--output", str(output)]
                with mock.patch.object(sys, "argv", argv), \
                        mock.patch.object(MODULE.subprocess, "Popen") as birth, \
                        mock.patch.object(MODULE, "compose_config") as dispatch, \
                        contextlib.redirect_stderr(io.StringIO()):
                    self.assertEqual(MODULE.main(), 1)
                    birth.assert_not_called()
                    dispatch.assert_not_called()
                self.assertFalse(output.exists())

    def test_default_main_interrupts_actual_stalled_read_at_original_cutoff(self):
        for clock in ("monotonic", "time"):
            with self.subTest(clock=clock), tempfile.TemporaryDirectory() as root:
                raw = json.dumps({"authority": "ROOT", "operation": "C_RENDER", "passive_native_custody_allowed": True}).encode()
                path = Path(root) / "approval.json"
                path.write_bytes(raw)
                output = Path(root) / "absent-output"
                argv = ["runner", "--approval", str(path), "--approval-sha256", MODULE.digest(raw), "--output", str(output)]
                real_clock = getattr(MODULE.time, clock)
                real_monotonic = MODULE.time.monotonic
                real_read = os.read
                origin_pending = [True]
                entered = []
                reader, writer = os.pipe()
                previous_handler = signal.getsignal(signal.SIGALRM)

                def original_origin():
                    value = real_clock()
                    if origin_pending[0]:
                        origin_pending[0] = False
                        return value - 89.6
                    return value

                def stalled(fd, count):
                    entered.append(fd)
                    return real_read(reader, 1)

                began = real_monotonic()
                try:
                    with mock.patch.object(sys, "argv", argv), \
                            mock.patch.object(MODULE.time, clock, side_effect=original_origin), \
                            mock.patch.object(MODULE.os, "read", side_effect=stalled), \
                            mock.patch.object(MODULE.subprocess, "Popen") as birth, \
                            mock.patch.object(MODULE, "compose_config") as dispatch, \
                            contextlib.redirect_stderr(io.StringIO()) as errors:
                        self.assertEqual(MODULE.main(), 1)
                        birth.assert_not_called()
                        dispatch.assert_not_called()
                    self.assertTrue(entered)
                    self.assertFalse(output.exists())
                    self.assertEqual(errors.getvalue(), "ROOT approval rejected before operator setup\n")
                    self.assertLess(real_monotonic() - began, 2)
                    self.assertEqual(signal.getsignal(signal.SIGALRM), previous_handler)
                    self.assertEqual(signal.getitimer(signal.ITIMER_REAL), (0.0, 0.0))
                finally:
                    os.close(reader)
                    os.close(writer)

    def test_delivery_rejects_symlink_above_declared_file_or_directory_root(self):
        for boundary in ("file", "directory"):
            with self.subTest(boundary=boundary), tempfile.TemporaryDirectory() as root:
                runner = self.make_runner(root)
                approved, outside = Path(root) / "approved", Path(root) / "outside"
                approved.mkdir()
                outside.mkdir()
                (outside / "tree").mkdir()
                (approved / "alias").symlink_to(outside, target_is_directory=True)
                alias_root = approved / "alias/tree"
                if boundary == "file":
                    (outside / "tree/config.json").write_bytes(b"actual independently pinned bytes")
                    member = alias_root / "config.json"
                    members = ["config.json"]
                else:
                    member = approved / "unaliased.json"
                    member.write_bytes(b"actual independently pinned bytes")
                    members = []
                approval = {"no_host_source_writers": True,
                            "delivery_files": [{"path": str(member), "bytes": member.stat().st_size,
                                                "sha256": MODULE.digest(member.read_bytes())}],
                            "delivery_directories": [{"path": str(alias_root), "members": members, "directories": []}]}
                with mock.patch.object(MODULE, "DAEMON_SOURCE_PREFIX", str(approved) + "/"):
                    with self.assertRaises((MODULE.Failure, OSError)):
                        MODULE.delivery_snapshot(runner, approval, {str(alias_root)})

    def test_owned_timeout_is_fail_with_actual_eof_and_reap(self):
        with tempfile.TemporaryDirectory() as root:
            runner = self.make_runner(root)
            with self.assertRaises(MODULE.Failure):
                runner.call([sys.executable, "-c", "import time;print('started',flush=True);time.sleep(10)"], seconds=1)
            record = runner.records[0]
            self.assertEqual(record["first_fault"], "native deadline")
            self.assertTrue(record["released"])
            self.assertEqual(record["raw"][0], b"started\n")
            self.assertFalse(runner.pending)

    def test_directory_fsync_failure_retains_both_bytes_and_task_failure(self):
        with tempfile.TemporaryDirectory() as root:
            runner = self.make_runner(root)
            original = os.fsync

            def sync(fd):
                if stat.S_ISDIR(os.fstat(fd).st_mode):
                    raise OSError("synthetic directory sync fault")
                original(fd)

            with mock.patch.object(MODULE.os, "fsync", side_effect=sync):
                with self.assertRaises(MODULE.Failure):
                    runner.call([sys.executable, "-c", "import sys;print('out');print('err',file=sys.stderr)"])
                self.assertEqual(runner.finish(), 1)
            self.assertEqual(runner.records[0]["raw"], (b"out\n", b"err\n"))
            self.assertTrue(runner.records[0]["released"])
            self.assertEqual((runner.output / "001.stdout").read_bytes(), b"out\n")
            self.assertEqual((runner.output / "001.stderr").read_bytes(), b"err\n")
            self.assertIn("terminal storage fault", runner.later_faults)

    def test_later_operator_and_unexpected_exit_faults_survive(self):
        with tempfile.TemporaryDirectory() as root:
            runner = self.make_runner(root)
            with self.assertRaises(MODULE.Failure):
                runner.call([sys.executable, "-c", "import sys,time;sys.stdout.write('x'*140000);sys.stdout.flush();time.sleep(.1);sys.exit(9)"])
            record = runner.records[0]
            self.assertEqual(record["first_fault"], "native capture overflow")
            self.assertIn("unexpected native exit", record["later_faults"])
            runner.fault("synthetic cleanup ownership fault")
            self.assertEqual(runner.finish(), 1)
            terminal = json.loads((runner.output / "terminal.json").read_bytes())
            self.assertEqual(terminal["failure"], "native capture overflow")
            self.assertIn("synthetic cleanup ownership fault", terminal["later_faults"])

    def test_successful_sync_cannot_accept_expired_publication(self):
        for clock in ("monotonic", "time"):
            for phase in ("native-first", "native-second", "terminal", "render", "delivery-after"):
                with self.subTest(clock=clock, phase=phase), tempfile.TemporaryDirectory() as root:
                    runner = self.make_runner(root)
                    real_clock = getattr(MODULE.time, clock)
                    real_sync = os.fsync
                    delay = [0]
                    syncs = [0]

                    def sync(fd):
                        real_sync(fd)
                        syncs[0] += 1
                        if not phase.startswith("native") or syncs[0] == (1 if phase == "native-first" else 3):
                            delay[0] = 2 if phase.startswith("native") else 10

                    with mock.patch.object(MODULE.time, clock, side_effect=lambda: real_clock() + delay[0]), \
                            mock.patch.object(MODULE.os, "fsync", side_effect=sync):
                        if phase.startswith("native"):
                            with self.assertRaises(MODULE.Failure):
                                runner.call([sys.executable, "-c", "import sys;print('out');print('err',file=sys.stderr)"], seconds=1)
                            record = runner.records[0]
                            self.assertEqual(record["raw"], (b"out\n", b"err\n"))
                            self.assertTrue(record["released"])
                            self.assertEqual((runner.output / "001.stdout").read_bytes(), b"out\n")
                            if phase == "native-first":
                                self.assertFalse((runner.output / "001.stderr").exists())
                                self.assertEqual(syncs[0], 1)
                            else:
                                self.assertEqual((runner.output / "001.stderr").read_bytes(), b"err\n")
                            self.assertEqual(record["first_fault"], "native publication deadline")
                        elif phase == "terminal":
                            self.assertEqual(runner.finish(), 1)
                            terminal = json.loads((runner.output / "terminal.json").read_bytes())
                            self.assertIsNone(terminal["pass"])
                            self.assertTrue(terminal["eligible_before_publication"])
                        elif phase == "render":
                            compose = Path(root) / "compose.json"
                            compose.write_bytes(b"synthetic authored input")
                            approval = {"operation": "C_RENDER", "project": "synthetic-qa-test",
                                        "daemon_id": "approved", "compose_file": str(compose),
                                        "compose_sha256": MODULE.digest(compose.read_bytes())}
                            runner.docker = lambda *args, **kw: b"approved\n" if args[0] == "info" else b'{"services":{}}'
                            with self.assertRaises(MODULE.Failure):
                                MODULE.compose_config(runner, approval)
                        else:
                            with self.assertRaises(MODULE.Failure):
                                runner.save("delivery-after.json", b"[]", cleanup=True)
                    self.assertIsNotNone(runner.first)
                    self.assertEqual(runner.finish(), 1)

    def test_actual_compose_and_maintenance_reads_are_physical_bounded_bytes(self):
        for consumer in ("compose", "maintenance"):
            maximum = 1048576 if consumer == "compose" else 131072
            for fault in ("valid", "fifo", "leaf-link", "ancestor-link", "hardlink", "oversize", "pin"):
                with self.subTest(consumer=consumer, fault=fault), tempfile.TemporaryDirectory() as root:
                    runner = self.make_runner(root)
                    directory = Path(root) / "inputs"
                    directory.mkdir()
                    path = directory / "source"
                    raw = b"# arbitrary non-JSON YAML/source bytes\n".ljust(maximum, b"x")
                    if fault == "oversize":
                        raw = b"x" * (maximum + 1)
                    path.write_bytes(raw)
                    pin = MODULE.digest(raw)
                    if fault == "fifo":
                        path.unlink()
                        os.mkfifo(path)
                    elif fault == "leaf-link":
                        target = directory / "target"
                        path.rename(target)
                        path.symlink_to(target)
                    elif fault == "ancestor-link":
                        alias = Path(root) / "alias"
                        alias.symlink_to(directory, target_is_directory=True)
                        path = alias / "source"
                    elif fault == "hardlink":
                        os.link(path, directory / "alias")
                    elif fault == "pin":
                        pin = "0" * 64
                    calls = []

                    def docker(*args, **kwargs):
                        calls.append(args)
                        if args[0] == "info":
                            return b"approved"
                        if args[:2] == ("image", "inspect"):
                            return json.dumps([{"Id": "sha256:c7776d61291b599bb4dd2f30eb6e7f33f2336e3b88ae800cb1d580e01661467e"}]).encode()
                        if args[0] == "compose":
                            return b'{"services":{}}'
                        if args[0] == "create":
                            raise MODULE.Failure("positive fixture reached CREATE")
                        return b""

                    runner.docker = docker
                    approval = {"project": "synthetic-qa-test", "daemon_id": "approved",
                                "operation": "C_RENDER", "compose_file": str(path), "compose_sha256": pin}
                    targets = ("/input/run-maintenance.py", "/input/test-maintenance.py",
                               "/transport/run-qualified.py", "/transport/guards.py")
                    approval["inputs"] = [{"operator_path": str(path), "sha256": pin, "target": target,
                                           "daemon_source": "/run/desktop/mnt/host/c/Users/ddriz/Projects/zns-chatbot/qa.local/synthetic/" + str(i)}
                                          for i, target in enumerate(targets)]
                    operation = MODULE.compose_config if consumer == "compose" else MODULE.maintenance_controls
                    if fault == "valid" and consumer == "compose":
                        operation(runner, approval)
                        self.assertTrue((runner.output / "rendered-compose.json").exists())
                    else:
                        with self.assertRaises((MODULE.Failure, OSError)):
                            operation(runner, approval)
                    if fault == "valid":
                        self.assertTrue(any(call[0] == ("compose" if consumer == "compose" else "create") for call in calls))
                    else:
                        self.assertFalse(any(call[0] in ("compose", "create") for call in calls))
                        self.assertEqual(list(runner.output.iterdir()), [])

    def test_actual_raw_consumers_interrupt_stalled_reads_on_original_clocks(self):
        for consumer in ("compose", "maintenance"):
            for clock in ("monotonic", "time"):
                with self.subTest(consumer=consumer, clock=clock), tempfile.TemporaryDirectory() as root:
                    runner = self.make_runner(root)
                    setattr(runner, "work_end" if clock == "monotonic" else "work_utc_end",
                            getattr(MODULE.time, clock)() + .15)
                    path = Path(root) / "source"
                    path.write_bytes(b"actual regular file")
                    pin = MODULE.digest(path.read_bytes())
                    identity = path.stat().st_ino
                    read_fd, write_fd = os.pipe()
                    real_read = os.read
                    calls = []

                    def blocked(fd, size):
                        return real_read(read_fd if os.fstat(fd).st_ino == identity else fd, size)

                    def docker(*args, **kwargs):
                        calls.append(args)
                        if args[0] == "info":
                            return b"approved"
                        return json.dumps([{"Id": "sha256:c7776d61291b599bb4dd2f30eb6e7f33f2336e3b88ae800cb1d580e01661467e"}]).encode()

                    runner.docker = docker
                    approval = {"project": "synthetic-qa-test", "daemon_id": "approved",
                                "compose_file": str(path), "compose_sha256": pin}
                    approval["inputs"] = [{"operator_path": str(path), "sha256": pin, "target": target}
                                          for target in ("/input/run-maintenance.py", "/input/test-maintenance.py",
                                                         "/transport/run-qualified.py", "/transport/guards.py")]
                    try:
                        with mock.patch.object(MODULE.os, "read", side_effect=blocked):
                            with self.assertRaisesRegex(MODULE.Failure, "original intake deadline"):
                                (MODULE.compose_config if consumer == "compose" else MODULE.maintenance_controls)(runner, approval)
                        self.assertFalse(any(call[0] in ("compose", "create") for call in calls))
                        self.assertEqual(list(runner.output.iterdir()), [])
                    finally:
                        os.close(read_fd)
                        os.close(write_fd)

    def test_default_main_rejects_output_alias_and_source_overlap_before_writes(self):
        for fault in ("ancestor-link", "source-root", "linked-source-unlinked-output"):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as root:
                source = Path(root) / "source"
                source.mkdir()
                member = source / "input"
                member.write_bytes(b"unchanged")
                output = source / "fresh"
                declared_source = source
                if fault == "ancestor-link":
                    alias = Path(root) / "alias"
                    alias.symlink_to(source, target_is_directory=True)
                    output = alias / "fresh"
                elif fault == "linked-source-unlinked-output":
                    alias = Path(root) / "alias"
                    alias.symlink_to(source, target_is_directory=True)
                    declared_source = alias
                raw = json.dumps({"authority": "ROOT", "operation": "C_RENDER", "passive_native_custody_allowed": True,
                                  "delivery_directories": [{"path": str(declared_source)}]}).encode()
                approval = Path(root) / "approval.json"
                approval.write_bytes(raw)
                argv = ["runner", "--approval", str(approval), "--approval-sha256", MODULE.digest(raw), "--output", str(output)]
                with mock.patch.object(sys, "argv", argv), mock.patch.object(MODULE, "compose_config") as dispatch, \
                        mock.patch.object(MODULE.subprocess, "Popen") as child, contextlib.redirect_stderr(io.StringIO()):
                    self.assertEqual(MODULE.main(), 1)
                    dispatch.assert_not_called()
                    child.assert_not_called()
                self.assertEqual(list(source.iterdir()), [member])
                self.assertEqual(member.read_bytes(), b"unchanged")
                self.assertFalse(output.exists())

    def test_actual_bound_mount_alias_and_backing_parent_reject_before_writes(self):
        aliases = ("/alias-source", "/alias-parent")
        output = Path(__file__).parent / "denied-alias-output"
        self.assertFalse(output.exists())
        for alias in aliases:
            with self.subTest(alias=alias):
                with self.assertRaisesRegex(MODULE.Failure, "physical output/source separation before writes"):
                    MODULE.Runner(output, sources=[alias])
                self.assertFalse(output.exists())
                with tempfile.TemporaryDirectory() as root:
                    approval = Path(root) / "approval.json"
                    raw = json.dumps({"authority": "ROOT", "operation": "C_RENDER",
                                      "passive_native_custody_allowed": True,
                                      "delivery_directories": [{"path": alias}]}).encode()
                    approval.write_bytes(raw)
                    argv = ["runner", "--approval", str(approval), "--approval-sha256", MODULE.digest(raw),
                            "--output", str(output)]
                    with mock.patch.object(sys, "argv", argv), mock.patch.object(MODULE, "compose_config") as dispatch, \
                            mock.patch.object(MODULE.subprocess, "Popen") as child, contextlib.redirect_stderr(io.StringIO()):
                        self.assertEqual(MODULE.main(), 1)
                        dispatch.assert_not_called()
                        child.assert_not_called()
                    self.assertFalse(output.exists())

    def test_all_empty_failed_create_retains_c_and_d_unknown_custody(self):
        for consumer in ("C", "D"):
            with self.subTest(consumer=consumer), tempfile.TemporaryDirectory() as root:
                runner = self.make_runner(root)
                runner.create_compose_pin, runner.delivery_sources = "0" * 64, set()
                project = "synthetic-qa-test"
                expected, _ = self.created_fixture(project, "app", "a" * 64)
                calls = []
                image = "sha256:c7776d61291b599bb4dd2f30eb6e7f33f2336e3b88ae800cb1d580e01661467e"

                def native(*args, **kwargs):
                    calls.append(args)
                    if args[0] in ("compose", "create"):
                        raise MODULE.Failure("first unknown CREATE")
                    if args[0] == "info":
                        return b"approved-daemon"
                    if args[0] == "image":
                        return json.dumps([{"Id": image}]).encode()
                    self.assertEqual(args[0], "ps")
                    return b""

                runner.docker = native
                inputs = [{"target": target, "operator_path": "/input/source",
                           "daemon_source": MODULE.DAEMON_SOURCE_PREFIX + "qa.local/public/source",
                           "sha256": "0" * 64}
                          for target in ("/input/run-maintenance.py", "/input/test-maintenance.py",
                                         "/transport/run-qualified.py", "/transport/guards.py")]
                with mock.patch.object(MODULE, "compose_config", return_value=(project, ["compose"], {"app": expected}, [])), \
                        mock.patch.object(MODULE, "read_pinned", return_value={}), \
                        mock.patch.object(MODULE, "read_bytes_pinned", return_value=b""), \
                        mock.patch.object(MODULE, "delivery_snapshot", return_value=[]):
                    with self.assertRaisesRegex(MODULE.Failure, "unknown attempted"):
                        if consumer == "C":
                            MODULE.preflight(runner, {"owner": "owner"})
                        else:
                            MODULE.maintenance_controls(runner, {"project": project,
                                                                 "daemon_id": "approved-daemon", "inputs": inputs})
                self.assertEqual(runner.first, "first unknown CREATE")
                self.assertTrue(runner.unresolved_resources)
                self.assertFalse(any(args[0] in ("inspect", "rm", "start") for args in calls))
                self.assertFalse(any(any(arg.startswith("id=") for arg in args) for args in calls))
                class Held(Exception):
                    pass
                with mock.patch.object(MODULE.time, "sleep", side_effect=Held):
                    with self.assertRaises(Held):
                        runner.finish()
                self.assertTrue(runner.unresolved_resources)

    def test_actual_birth_stall_retains_unknown_child_before_handle_return(self):
        for clock in ("monotonic", "time"):
            with self.subTest(delayed_worker_clock=clock), tempfile.TemporaryDirectory() as root:
                runner = self.make_runner(root)
                release, entered = MODULE.threading.Event(), MODULE.threading.Event()
                original_run, real_clock = MODULE.threading.Thread.run, getattr(MODULE.time, clock)
                delay = [0]

                def delayed(thread):
                    entered.set()
                    delay[0] = 10
                    release.wait()
                    original_run(thread)

                try:
                    with mock.patch.object(MODULE.threading.Thread, "run", delayed), \
                            mock.patch.object(MODULE.subprocess.Popen, "__init__") as constructor, \
                            mock.patch.object(MODULE.time, clock, side_effect=lambda: real_clock() + delay[0]):
                        with self.assertRaisesRegex(MODULE.Failure, "native birth deadline"):
                            runner.call([sys.executable, "-c", "raise AssertionError('never entered')"], seconds=2)
                        self.assertTrue(entered.is_set())
                        self.assertEqual(len(runner.pending), 1)
                        owner, record = runner.pending[0], runner.records[0]
                        self.assertTrue(owner._qa_acquiring_thread.is_alive())
                        self.assertFalse(owner._qa_no_birth)
                        self.assertEqual(record["acquisition"], "unknown")
                        self.assertFalse(record["released"])
                        constructor.assert_not_called()
                        for stream in owner._qa_readers:
                            os.fstat(stream.fileno())
                        for fd in owner._qa_writers:
                            os.fstat(fd)
                        with self.assertRaisesRegex(MODULE.Failure, "custody forbids dispatch"):
                            runner.call([sys.executable, "-c", "pass"], cleanup=True)
                        delay[0] = 10
                        release.set()
                        owner._qa_acquiring_thread.join(timeout=1)
                        self.assertFalse(owner._qa_acquiring_thread.is_alive())
                        self.assertTrue(owner._qa_no_birth)
                        constructor.assert_not_called()
                    self.assertEqual(runner.finish(), 1)
                    self.assertEqual(record["acquisition"], "not-started")
                    self.assertTrue(record["no_native_birth"])
                    self.assertTrue(record["released"])
                    self.assertEqual(record["raw"], (b"", b""))
                    self.assertIsNone(record["pid"])
                    self.assertIsNone(record["exit"])
                    self.assertFalse(record["reaped"])
                    self.assertEqual(record["eof"], [False, False])
                    self.assertEqual(record["readers_closed"], [True, True])
                    self.assertTrue(record["selector_closed"])
                    self.assertFalse(owner._qa_writers)
                    self.assertFalse(owner._qa_reader_fds)
                    self.assertFalse(runner.pending)
                    self.assertEqual(runner.first, "native birth deadline")
                    with mock.patch.object(MODULE.subprocess.Popen, "__init__") as constructor, \
                            self.assertRaisesRegex(MODULE.Failure, "prior failure"):
                        runner.call([sys.executable, "-c", "pass"])
                    constructor.assert_not_called()
                finally:
                    release.set()
                    if runner.pending:
                        owner = runner.pending[0]
                        owner._qa_acquiring_thread.join(timeout=1)
                        for fd in owner._qa_writers:
                            os.close(fd)
                        for stream in owner._qa_readers:
                            stream.close()
                        owner._qa_selector.close()
        for boundary in ("before-pid", "exec-pipe"):
            with self.subTest(boundary=boundary), tempfile.TemporaryDirectory() as root:
                runner = self.make_runner(root)
                reader, writer = os.pipe()
                original_fork, original_read = MODULE.subprocess._fork_exec, os.read
                born = {}

                def fork(*args):
                    self.assertEqual(args[19], os.geteuid())
                    pid = original_fork(*args)
                    born.update(pid=pid, error_pipe=args[12])
                    self.assertEqual(len(runner.pending), 1)
                    for stream in runner.pending[0]._qa_readers:
                        self.assertFalse(stream.closed)
                        os.fstat(stream.fileno())
                    self.assertIsNone(runner.pending[0].pid)
                    if boundary == "before-pid":
                        original_read(reader, 1)
                    return pid

                def read(fd, count):
                    if boundary == "exec-pipe" and fd == born.get("error_pipe"):
                        self.assertEqual(runner.pending[0].pid, born["pid"])
                        return original_read(reader, 1)
                    return original_read(fd, count)

                start = MODULE.time.monotonic()
                try:
                    with mock.patch.object(MODULE.subprocess, "_fork_exec", side_effect=fork), \
                            mock.patch.object(MODULE.os, "read", side_effect=read):
                        with self.assertRaisesRegex(MODULE.Failure, "native birth deadline"):
                            runner.call([sys.executable, "-c", "import time; time.sleep(10)"], seconds=.15)
                    self.assertLess(MODULE.time.monotonic() - start, 1)
                    self.assertEqual(len(runner.pending), 1)
                    record = runner.records[0]
                    self.assertEqual(record["acquisition"], "unknown")
                    self.assertFalse(record["released"])
                    self.assertFalse(record["reaped"])
                    self.assertEqual(record["eof"], [False, False])
                    self.assertEqual(record["raw"], (b"", b""))
                    self.assertEqual(runner.first, "native birth deadline")
                    self.assertEqual(record["pid"], None if boundary == "before-pid" else born["pid"])
                    with mock.patch.object(MODULE.subprocess, "Popen") as next_child:
                        with self.assertRaisesRegex(MODULE.Failure, "custody forbids dispatch"):
                            runner.call([sys.executable, "-c", "raise SystemExit(0)"], cleanup=True)
                        next_child.assert_not_called()
                    class Held(Exception):
                        pass
                    with mock.patch.object(MODULE.time, "sleep", side_effect=Held):
                        with self.assertRaises(Held):
                            runner.finish()
                    self.assertEqual(len(runner.pending), 1)
                finally:
                    # The fixture knows its exact fork return; production does not
                    # inherit this knowledge or claim the unresolved object released.
                    if "pid" in born:
                        os.kill(born["pid"], signal.SIGKILL)
                        self.assertEqual(os.waitpid(born["pid"], 0)[0], born["pid"])
                    os.close(reader)
                    os.close(writer)
                    owner = runner.pending[0]
                    owner._qa_acquiring_thread.join(timeout=1)
                    self.assertFalse(owner._qa_acquiring_thread.is_alive())
                    for fd in owner._qa_writers:
                        os.close(fd)
                    for stream in owner._qa_readers:
                        stream.close()

    def test_actual_preexec_kernel_wait_keeps_observer_readers_and_uid(self):
        self.assertEqual((os.getuid(), os.geteuid()), (0, 0))
        for constructor_error in (False, True):
            with self.subTest(constructor_error=constructor_error), tempfile.TemporaryDirectory() as root:
                runner = self.make_runner(root)
                reader, writer = os.pipe()
                ready_reader, ready_writer = os.pipe()
                original_fork = MODULE.subprocess._fork_exec
                born = {}

                def blocked_child():
                    os.write(1, b"preexec stdout")
                    os.write(2, b"preexec stderr")
                    os.write(ready_writer, b"ready")
                    os.read(reader, 1)

                def fork(*args):
                    self.assertEqual(args[19], 0)
                    self.assertIsNone(args[21])
                    changed = list(args)
                    changed[21] = blocked_child
                    pid = original_fork(*changed)
                    born["pid"] = pid
                    if constructor_error:
                        os.read(ready_reader, 5)
                        raise OSError("private-constructor-token-synthetic")
                    return pid

                started = MODULE.time.monotonic()
                try:
                    with mock.patch.object(MODULE.subprocess, "_fork_exec", side_effect=fork):
                        expected = "native acquisition failed" if constructor_error else "native birth deadline"
                        with self.assertRaisesRegex(MODULE.Failure, expected):
                            runner.call([sys.executable, "-c", "raise SystemExit(0)"], seconds=.4)
                    self.assertLess(MODULE.time.monotonic() - started, 1.5)
                    owner, record = runner.pending[0], runner.records[0]
                    self.assertEqual(owner._qa_acquiring_thread.is_alive(), not constructor_error)
                    self.assertEqual(owner.pid, None if constructor_error else born["pid"])
                    if constructor_error:
                        self.assertEqual(record["acquisition_error"], "native constructor failure")
                        self.assertIn("private-constructor-token-synthetic", str(owner._qa_error))
                        self.assertNotIn("private-constructor-token-synthetic", json.dumps(record, default=str))
                    self.assertEqual(record["raw"], (b"preexec stdout", b"preexec stderr"))
                    self.assertEqual(record["eof"], [False, False])
                    self.assertFalse(record["reaped"])
                    self.assertFalse(record["released"])
                    for stream in owner._qa_readers:
                        self.assertFalse(stream.closed)
                        os.fstat(stream.fileno())
                    with mock.patch.object(MODULE.subprocess, "Popen") as next_child:
                        with self.assertRaisesRegex(MODULE.Failure, "custody forbids dispatch"):
                            runner.call([sys.executable, "-c", "raise SystemExit(0)"], cleanup=True)
                        next_child.assert_not_called()
                    if constructor_error:
                        class Held(Exception):
                            pass
                        with mock.patch.object(MODULE.time, "sleep", side_effect=Held), self.assertRaises(Held):
                            runner.finish()
                        terminal = (runner.output / "terminal.json").read_bytes()
                        self.assertNotIn(b"private-constructor-token-synthetic", terminal)
                        self.assertIn(b"native constructor failure", terminal)
                finally:
                    if "pid" in born:
                        os.kill(born["pid"], signal.SIGKILL)
                        self.assertEqual(os.waitpid(born["pid"], 0)[0], born["pid"])
                    os.close(reader)
                    os.close(writer)
                    os.close(ready_reader)
                    os.close(ready_writer)
                    if runner.pending:
                        owner = runner.pending[0]
                        owner._qa_acquiring_thread.join(timeout=1)
                        self.assertFalse(owner._qa_acquiring_thread.is_alive())
                        for fd in owner._qa_writers:
                            os.close(fd)
                        for stream in owner._qa_readers:
                            stream.close()

    def test_actual_reader_close_fault_keeps_fd_and_closes_other_reader(self):
        for failed in ({0}, {1}, {0, 1}):
            with self.subTest(failed=failed), tempfile.TemporaryDirectory() as root:
                runner = self.make_runner(root)
                original_open, streams, closes = os.fdopen, [], []

                class Reader:
                    def __init__(self, raw, index):
                        self.raw, self.index = raw, index
                    def fileno(self):
                        return self.raw.fileno()
                    def close(self):
                        closes.append(self.index)
                        if self.index in failed:
                            raise OSError("controlled reader close")
                        self.raw.close()

                def opened(fd, *args, **kwargs):
                    wrapped = Reader(original_open(fd, *args, **kwargs), len(streams))
                    streams.append(wrapped)
                    return wrapped

                try:
                    with mock.patch.object(MODULE.os, "fdopen", side_effect=opened):
                        with self.assertRaisesRegex(MODULE.Failure, "native reader close custody fault"):
                            runner.call([sys.executable, "-c",
                                         "import os; os.write(1,b'out'); os.write(2,b'err')"], seconds=3)
                    record = runner.records[0]
                    self.assertEqual(record["raw"], (b"out", b"err"))
                    self.assertTrue(record["reaped"])
                    self.assertEqual(record["eof"], [True, True])
                    self.assertEqual(closes, [0, 1])
                    self.assertEqual(record["readers_closed"], [index not in failed for index in range(2)])
                    self.assertFalse(record["released"])
                    self.assertEqual(len(runner.pending), 1)
                    for index in failed:
                        fd = streams[index].fileno()
                        os.fstat(fd)
                        self.assertIn(fd, runner.pending[0]._qa_reader_fds)
                    with mock.patch.object(MODULE.subprocess, "Popen") as next_child:
                        for cleanup in (False, True):
                            with self.assertRaisesRegex(MODULE.Failure, "custody forbids dispatch"):
                                runner.call([sys.executable, "-c", "raise SystemExit(0)"], cleanup=cleanup)
                        next_child.assert_not_called()
                    class Held(Exception):
                        pass
                    with mock.patch.object(MODULE.time, "sleep", side_effect=Held):
                        with self.assertRaises(Held):
                            runner.finish()
                finally:
                    for stream in streams:
                        stream.raw.close()

    def test_actual_duplicate_bound_source_members_reject_before_output(self):
        sources = [Path(__file__).parent / "run.py", Path("/alias-source/run.py")]
        for path in sources:
            self.assertEqual(path.stat().st_nlink, 1)
        self.assertEqual((sources[0].stat().st_dev, sources[0].stat().st_ino),
                         (sources[1].stat().st_dev, sources[1].stat().st_ino))
        with tempfile.TemporaryDirectory() as root:
            output = Path(root) / "disjoint"
            for admitted in (sources, [Path(__file__).parent, Path("/alias-source")]):
                with self.subTest(admitted=admitted), \
                        self.assertRaisesRegex(MODULE.Failure, "duplicate physical source before writes"):
                    MODULE.Runner(output, sources=admitted)
                self.assertFalse(output.exists())
            runner = self.make_runner(root)
            approval = {"no_host_source_writers": True, "delivery_directories": [],
                        "delivery_files": [{"path": str(path), "bytes": path.stat().st_size,
                                            "sha256": MODULE.digest(path.read_bytes())} for path in sources]}
            with mock.patch.object(MODULE, "DAEMON_SOURCE_PREFIX", "/"), \
                    self.assertRaisesRegex(MODULE.Failure, "duplicate physical delivered file"):
                MODULE.delivery_snapshot(runner, approval, set(map(str, sources)))

    def test_actual_signal_interrupts_blocked_publication_operations(self):
        for operation in ("open", "write", "file-sync", "directory-sync"):
            with self.subTest(operation=operation), tempfile.TemporaryDirectory() as root:
                runner = self.make_runner(root)
                reader, writer = os.pipe()
                real_read, real_open, real_sync = os.read, os.open, os.fsync
                start = MODULE.time.monotonic()

                def blocked_open(name, *args, **kwargs):
                    if name == "proof":
                        return real_read(reader, 1)
                    return real_open(name, *args, **kwargs)

                def blocked_sync(fd):
                    is_directory = stat.S_ISDIR(os.fstat(fd).st_mode)
                    if is_directory == (operation == "directory-sync"):
                        return real_read(reader, 1)
                    return real_sync(fd)

                target, dependency = {
                    "open": ("open", blocked_open),
                    "write": ("write", lambda *_: real_read(reader, 1)),
                    "file-sync": ("fsync", blocked_sync),
                    "directory-sync": ("fsync", blocked_sync),
                }[operation]
                try:
                    with mock.patch.object(MODULE.os, target, side_effect=dependency):
                        with self.assertRaisesRegex(MODULE.Failure, "original intake deadline"):
                            runner.save("proof", b"retained fixture", end=start + 0.15,
                                        utc_end=MODULE.time.time() + 0.15)
                    self.assertLess(MODULE.time.monotonic() - start, 1.0)
                    self.assertFalse(runner.pending_writers)
                finally:
                    os.close(reader)
                    os.close(writer)

    def test_stalled_sync_uses_native_and_original_terminal_cutoffs(self):
        for terminal in (False, True):
            with self.subTest(terminal=terminal), tempfile.TemporaryDirectory() as root:
                runner = (MODULE.Runner(Path(root) / "receipts", active=0.2, cleanup=0.2)
                          if terminal else self.make_runner(root))
                reader, writer = os.pipe()
                real_read = os.read
                start = MODULE.time.monotonic()
                try:
                    with mock.patch.object(MODULE.os, "fsync", side_effect=lambda _: real_read(reader, 1)):
                        if terminal:
                            self.assertEqual(runner.finish(), 1)
                        else:
                            with self.assertRaises(MODULE.Failure):
                                runner.call([sys.executable, "-c", "print('retained')"], seconds=0.4)
                    self.assertLess(MODULE.time.monotonic() - start, 1.0)
                    self.assertFalse(runner.pending_writers)
                    self.assertFalse(runner.pending)
                    if terminal:
                        receipt = json.loads((runner.output / "terminal.json").read_bytes())
                        self.assertIsNone(receipt["pass"])
                        self.assertIsNotNone(runner.first)
                    else:
                        record = runner.records[0]
                        self.assertEqual(record["raw"], (b"retained\n", b""))
                        self.assertEqual(record["exit"], 0)
                        self.assertTrue(record["released"])
                        self.assertEqual(record["eof"], [True, True])
                        self.assertIsNotNone(record["first_fault"])
                        self.assertEqual(runner.finish(), 1)
                finally:
                    os.close(reader)
                    os.close(writer)

    def test_failed_writer_close_retains_handle_and_blocks_dispatch(self):
        with tempfile.TemporaryDirectory() as root:
            runner = self.make_runner(root)
            real_close = os.close

            def close(fd):
                if fd in runner.pending_writers:
                    raise OSError("controlled close failure")
                return real_close(fd)

            try:
                with mock.patch.object(MODULE.os, "close", side_effect=close):
                    with self.assertRaisesRegex(OSError, "controlled close failure"):
                        runner.save("proof", b"actual retained fd")
                self.assertEqual(len(runner.pending_writers), 1)
                self.assertEqual(os.fstat(runner.pending_writers[0]).st_size, len(b"actual retained fd"))
                with mock.patch.object(MODULE.subprocess, "Popen") as child:
                    with self.assertRaisesRegex(MODULE.Failure, "unresolved native custody"):
                        runner.call([sys.executable, "-c", "pass"], cleanup=True)
                    child.assert_not_called()
                with self.assertRaisesRegex(MODULE.Failure, "unresolved writer custody"):
                    runner.save("second", b"forbidden", cleanup=True)
                self.assertFalse((runner.output / "second").exists())
            finally:
                for fd in list(runner.pending_writers):
                    real_close(fd)
                    runner.pending_writers.remove(fd)

    def test_receipt_publication_refuses_replaced_output_ancestor(self):
        with tempfile.TemporaryDirectory() as root:
            parent = Path(root) / "parent"
            parent.mkdir()
            runner = self.make_runner(parent)
            parent.rename(Path(root) / "retained")
            foreign = Path(root) / "foreign"
            foreign.mkdir()
            (foreign / "receipts").mkdir()
            parent.symlink_to(foreign, target_is_directory=True)
            with self.assertRaises(OSError):
                runner.save("proof", b"must not write")
            self.assertFalse((foreign / "receipts/proof").exists())
            self.assertFalse((Path(root) / "retained/receipts/proof").exists())

    def test_actual_c_delivery_bytes_and_directory_members_are_bound(self):
        with tempfile.TemporaryDirectory() as root:
            runner = self.make_runner(root)
            path = Path(root) / "private"
            path.mkdir()
            member = path / "config.json"
            member.write_bytes(b"typed private fixture")
            item = {"path": str(member), "sha256": MODULE.digest(member.read_bytes())}
            manifest = {"inputs": {key: dict(item) for key in
                                   ("plan", "render", "images", "postgres_image", "postgres_config", "d_constructor")}}
            for change in ("extra", "missing", "alias"):
                changed = json.loads(json.dumps(manifest))
                if change == "extra":
                    changed["inputs"]["extra"] = item
                elif change == "missing":
                    del changed["inputs"]["plan"]
                with self.subTest(actual=change), self.assertRaisesRegex(MODULE.Failure, "actual producer input"):
                    actual_inputs(changed)
            approval = {"no_host_source_writers": True,
                        "delivery_files": [{"path": str(member), "bytes": member.stat().st_size,
                                            "sha256": MODULE.digest(member.read_bytes())}],
                        "delivery_directories": [{"path": str(path), "members": ["config.json"], "directories": []}]}
            with mock.patch.object(MODULE, "DAEMON_SOURCE_PREFIX", root + "/"):
                before = MODULE.delivery_snapshot(runner, approval, {str(path)})
                self.assertEqual(before, MODULE.delivery_snapshot(runner, approval, {str(path)}))
                member.write_bytes(b"wrong private config")
                with self.assertRaises(MODULE.Failure):
                    MODULE.delivery_snapshot(runner, approval, {str(path)})
                member.write_bytes(b"typed private fixture")
                for name, directory in (("extra", True), ("extra.py", False), ("config.pyc", False),
                                        ("__pycache__", True)):
                    extra = path / name
                    extra.mkdir() if directory else extra.write_bytes(b"unadmitted import bytes")
                    with self.subTest(name=name), self.assertRaises(MODULE.Failure):
                        MODULE.delivery_snapshot(runner, approval, {str(path)})
                    extra.rmdir() if directory else extra.unlink()
                approval["no_host_source_writers"] = False
                with self.assertRaises(MODULE.Failure):
                    MODULE.delivery_snapshot(runner, approval, {str(path)})
            # Parsing consumes the authenticated snapshot even if the pathname changes afterwards.
            authenticated = b'{"authority":"ROOT","value":"authenticated"}'
            substituted = b'{"authority":"foreign","value":"substituted"}'
            member.write_bytes(authenticated)
            real_loads = MODULE.json.loads

            def parse(raw, *args, **kwargs):
                member.write_bytes(substituted)
                self.assertEqual(raw, authenticated)
                return real_loads(raw, *args, **kwargs)

            with mock.patch.object(MODULE.json, "loads", side_effect=parse):
                self.assertEqual(MODULE.read_pinned(member, MODULE.digest(authenticated)),
                                 {"authority": "ROOT", "value": "authenticated"})
            self.assertEqual(member.read_bytes(), substituted)
            with self.assertRaises(MODULE.Failure):
                MODULE.read_pinned(member, MODULE.digest(authenticated))

    def test_create_uses_frozen_render_not_changed_secondary_input(self):
        with tempfile.TemporaryDirectory() as root:
            runner = self.make_runner(root)
            project = "synthetic-qa-test"
            compose = Path(root) / "compose.json"
            secondary = Path(root) / "secondary.env"
            compose.write_bytes(b"synthetic primary")
            secondary.write_bytes(b"original")
            service, expected, image, config = self.native_plan()
            expected["name"] = project + "-app-1"
            service["labels"] = {"synthetic.owner": "owner"}
            config["services"] = {"app": service}
            config["networks"]["database"]["labels"] = {"synthetic.owner": "owner"}
            raw = json.dumps(config).encode()
            approval = {"operation": "C_CREATED_PREFLIGHT", "project": project, "owner": "owner",
                        "daemon_id": "approved", "compose_file": str(compose),
                        "compose_sha256": MODULE.digest(compose.read_bytes()),
                        "rendered_config_sha256": MODULE.digest(raw), "services": {"app": expected},
                        "inactive_services": {}, "volumes": {},
                        "networks": {"database": {"rendered": config["networks"]["database"],
                                     "name": project + "_database", "driver": "bridge", "options": None,
                                     "internal": False}}}
            creates = []

            def native(*args, **kwargs):
                if args[0] == "info":
                    return b"approved\n"
                if args[:2] == ("image", "inspect"):
                    return json.dumps([image]).encode()
                if args[0] == "compose" and "config" in args:
                    secondary.write_bytes(b"changed secondary")
                    return raw
                if args[0] == "compose" and "create" in args:
                    creates.append(args)
                    frozen_path = Path(args[args.index("--file") + 1])
                    self.assertNotEqual(frozen_path, compose)
                    self.assertEqual(json.loads(frozen_path.read_bytes()), MODULE.literal_compose(config))
                    raise MODULE.Failure("synthetic prebirth stop")
                return b""

            runner.docker = native
            with mock.patch.object(MODULE, "plan"), mock.patch.object(MODULE, "delivery_snapshot", return_value=[]):
                with self.assertRaises(MODULE.Failure):
                    MODULE.preflight(runner, approval)
            self.assertEqual(len(creates), 1)
            self.assertNotIn("--profile", creates[0])
            self.assertEqual(secondary.read_bytes(), b"changed secondary")
            self.assertEqual(MODULE.literal_compose({"value": "$secret"}), {"value": "$$secret"})


if __name__ == "__main__":
    unittest.main()
