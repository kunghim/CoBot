import io
import json
import unittest
import zipfile
import runpy
from pathlib import Path

sanitize = runpy.run_path(str(Path(__file__).with_name("sanitize-test-evidence.py")))


class EvidenceTest(unittest.TestCase):
    def test_trace_redacts_auth_and_embedded_bodies_but_keeps_failure_details(self):
        records = [
            {"request": {"headers": [{"name": "Authorization", "value": "fixture-bearer"}],
                         "cookies": [{"name": "cobot_session_profile", "value": "fixture-session"}],
                         "postData": {"text": json.dumps({"secret": "fixture-bootstrap", "requestId": "test"})}}},
            {"error": {"message": "Expected 200, received 403"}, "status": 403},
            {"type": "log", "message": "  set-cookie: cobot_session_profile=fixture-log-session; HttpOnly"},
        ]
        raw = "\n".join(json.dumps(record) for record in records).encode()
        result = sanitize["sanitize_bytes"](raw).decode()
        for credential in ["fixture-bearer", "fixture-session", "fixture-bootstrap", "fixture-log-session"]:
            self.assertNotIn(credential, result)
        self.assertIn("Expected 200, received 403", result)
        self.assertIn("requestId", result)

    def test_archive_remains_readable_and_screenshots_are_unchanged(self):
        source = io.BytesIO()
        picture = b"\x89PNG\r\n\x1a\nsynthetic image"
        with zipfile.ZipFile(source, "w") as archive:
            archive.writestr("test.trace", '{"url":"http://localhost/#cobot-bootstrap=fixture-link"}\n')
            archive.writestr("resources/body.json", '{"bearerToken":"fixture-token","data":{"theme":"harbor"}}')
            archive.writestr("resources/image.png", picture)
        with zipfile.ZipFile(io.BytesIO(sanitize["sanitize_zip"](source.getvalue()))) as archive:
            self.assertNotIn(b"fixture-link", archive.read("test.trace"))
            self.assertNotIn(b"fixture-token", archive.read("resources/body.json"))
            self.assertIn(b"harbor", archive.read("resources/body.json"))
            self.assertEqual(archive.read("resources/image.png"), picture)


if __name__ == "__main__":
    unittest.main()
