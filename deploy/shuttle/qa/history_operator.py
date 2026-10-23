"""Supported history/archive JSON operations using the same local one-shot guards.

Raw gzip upload uses the documented /internal/admin/legacy/store contract;
account adoption remains the maintained shuttle-import apply-accounts command.
"""
import operator_cli as qa

qa.ACTIONS.update({
    "archive-prepare": ("POST", qa.AGENT + "store_archive/prepare", True, ("sha256",)),
    "archive-status": ("POST", qa.AGENT + "store_archive/status", False, ()),
    "archive-validate": ("POST", qa.AGENT + "validate_stored_archive", False, ("archive_id",)),
    "history-prepare": ("POST", qa.AGENT + "apply_history/prepare", True, ("archive_id", "sha256", "projection_version")),
    "history-confirm": ("POST", qa.AGENT + "apply_history", True, ("archive_id", "sha256", "projection_version")),
    "history-status": ("POST", qa.AGENT + "apply_history/status", False, ()),
    "history-page": ("POST", "/internal/agent/history/page", False, ()),
    "history-text": ("POST", "/internal/agent/history/text", False, ("id",)),
})


def main():
    qa.main()


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(qa.json.dumps({"stopped": True, "error_type": type(error).__name__, "no_automatic_retry": True}))
        raise SystemExit(1)
