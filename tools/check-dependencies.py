"""Check the resolved module graph against the reviewed dependency lock."""

import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def records(*args):
    result = subprocess.run(
        ["go", *args], cwd=ROOT, check=True, text=True, capture_output=True
    )
    decoder = json.JSONDecoder()
    tail = result.stdout.strip()
    while tail:
        item, end = decoder.raw_decode(tail)
        yield item
        tail = tail[end:].lstrip()


def main():
    lock = json.loads((ROOT / "tools/dependencies.lock.json").read_text())
    pins = {item["module"]: item for item in lock["modules"]}
    if len(pins) != len(lock["modules"]):
        raise SystemExit("duplicate module in dependency lock")
    graph = {
        item["Path"]: item
        for item in records("list", "-m", "-json", "all")
        if not item.get("Main")
    }
    if graph.keys() != pins.keys():
        raise SystemExit("module graph changed: review and update dependency lock")
    downloaded = {item["Path"]: item for item in records("mod", "download", "-json", "all")}
    for name, item in graph.items():
        pin = pins[name]
        if "Replace" in item or item["Version"] != pin["version"]:
            raise SystemExit(f"unreviewed module version or replacement: {name}")
        if not re.fullmatch(r"[0-9a-f]{40}", pin["commit"]):
            raise SystemExit(f"invalid full commit pin: {name}")
        artifact = downloaded[name]
        if artifact.get("Sum") != pin["sum"] or artifact.get("GoModSum") != pin["go_mod_sum"]:
            raise SystemExit(f"module checksums differ from reviewed lock: {name}")
        origin = artifact.get("Origin", {}).get("Hash")
        if origin and origin != pin["commit"]:
            raise SystemExit(f"module origin differs from reviewed lock: {name}")
    print(f"Verified {len(graph)} reviewed module versions and checksums.")


if __name__ == "__main__":
    main()
