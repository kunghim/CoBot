"""Redact temporary HTTP credentials from Playwright traces before upload.

Only the synthetic browser reports are inputs; runtime profiles are never
uploaded. Trace resource names stay intact so Trace Viewer can still open them.
"""
import io
import json
from pathlib import Path
import re
import sys
import zipfile


REDACTED = "<redacted>"
PRIVATE_KEYS = {
    "authorization", "cookie", "setcookie", "xcsrftoken", "xinstancesecret",
    "secret", "bootstrapsecret", "bearertoken", "accesstoken", "refreshtoken",
    "apikey", "csrftoken",
}


def scrub_text(value):
    # APIRequestContext also writes headers into human-readable trace logs.
    value = re.sub(
        r"(?im)((?:authorization|set-cookie|cookie|x-csrf-token|x-instance-secret):)[^\r\n]*",
        r"\1 " + REDACTED, value,
    )
    return re.sub(r"(cobot-bootstrap=)[^&#\s\"<>]+", r"\1" + REDACTED, value)


def scrub(value):
    if isinstance(value, list):
        return [scrub(item) for item in value]
    if isinstance(value, dict):
        # HAR headers/cookies are represented as {name, value} pairs.
        name = re.sub(r"[^a-z]", "", str(value.get("name", "")).lower())
        result = {}
        for key, child in value.items():
            normalized = re.sub(r"[^a-z]", "", key.lower())
            if normalized in PRIVATE_KEYS or (key == "value" and name in PRIVATE_KEYS):
                result[key] = REDACTED
            elif normalized == "cookies" and isinstance(child, list):
                result[key] = [scrub({**cookie, "value": REDACTED}) for cookie in child]
            else:
                result[key] = scrub(child)
        return result
    if isinstance(value, str):
        try:
            nested = json.loads(value)
        except (ValueError, TypeError):
            nested = None
        if isinstance(nested, (dict, list)):
            return json.dumps(scrub(nested), ensure_ascii=False)
        return scrub_text(value)
    return value


def sanitize_bytes(data):
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError:
        return data
    # Playwright .trace and .network files are newline-delimited JSON;
    # response bodies under resources/ can be ordinary, multiline JSON.
    try:
        return (json.dumps(scrub(json.loads(text)), ensure_ascii=False) + "\n").encode()
    except ValueError:
        pass
    lines = []
    for line in text.splitlines(keepends=True):
        try:
            lines.append(json.dumps(scrub(json.loads(line)), ensure_ascii=False) + "\n")
        except ValueError:
            lines.append(scrub_text(line))
    return "".join(lines).encode()


def sanitize_zip(data):
    output = io.BytesIO()
    with zipfile.ZipFile(io.BytesIO(data)) as source, zipfile.ZipFile(output, "w") as target:
        for entry in source.infolist():
            raw = source.read(entry)
            target.writestr(entry, sanitize_bytes(raw))
    return output.getvalue()


def sanitize_directory(directory):
    for path in Path(directory).rglob("*"):
        if path.is_file() and not path.is_symlink():
            data = path.read_bytes()
            if path.suffix == ".zip":
                path.write_bytes(sanitize_zip(data))
            elif path.suffix in {".json", ".trace", ".network", ".txt", ".md"}:
                path.write_bytes(sanitize_bytes(data))


if __name__ == "__main__":
    for directory in sys.argv[1:]:
        sanitize_directory(directory)
