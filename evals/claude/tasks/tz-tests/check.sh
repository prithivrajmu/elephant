TZ=UTC python3 test_report.py && grep -q localtime report.py && [ "$(shasum test_report.py | cut -d' ' -f1)" = "$(cat "$TASK_DIR/test_report.sha")" ]
