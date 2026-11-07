"""Single static entry point; default inspect is offline. No automatic chains."""
import importlib
import sys

COMMANDS = {
    "app": "operator_cli", "scenario": "scenario_db", "iam-human": "iam_human",
    "iam-role": "iam_roles", "history": "history_operator", "fixture": "history_fixture",
    "archive-upload": "archive_upload", "loom-history": "loom_history",
}


if __name__ == "__main__":
    try:
        if len(sys.argv) < 2 or sys.argv[1] == "inspect":
            module = "operator_cli"
            sys.argv = [sys.argv[0], "inspect"]
        else:
            module = COMMANDS[sys.argv[1]]
            sys.argv = [sys.argv[0], *sys.argv[2:]]
        importlib.import_module(module).main()
    except Exception as error:
        print('{"stopped":true,"error_type":"' + type(error).__name__ + '","no_automatic_retry":true}')
        raise SystemExit(1)
