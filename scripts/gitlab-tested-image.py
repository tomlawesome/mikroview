#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
"""Reads the evidence scripts/gitlab-record-tested-image.sh wrote (#1472).

Usage:
  python3 -I scripts/gitlab-tested-image.py digest <evidence.json> <repository>

Prints the recorded imageDigest, and nothing else, once the file proves
to be what publish:image wrote for *this* pipeline's image:

  - a JSON object whose imageDigest is sha256:<64 hex>;
  - whose imageReference is exactly <repository>@<imageDigest>, so a
    record of some other registry or path is refused rather than signed;
  - whose commit is $CI_COMMIT_SHA, whenever that is set, so an artifact
    from another pipeline's commit is refused too.

A file over 64 KiB is refused unread. Anything else exits 1 with the reason on stderr. The one reader for
sign:release-digest, publish:channel and record:upgrade-fixture, so the
three cannot come to disagree about what the evidence says.
"""
import json
import os
import re
import sys

DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
# The real record is a few hundred bytes. Anything over this is not one,
# and is refused before it is parsed rather than read whole.
MAX_BYTES = 64 * 1024


def fail(message):
    print(f"gitlab-tested-image: {message}", file=sys.stderr)
    sys.exit(1)


def main(argv):
    if len(argv) != 4 or argv[1] != "digest":
        fail("usage: gitlab-tested-image.py digest <evidence.json> <repository>")
    path, repository = argv[2], argv[3]
    if not repository:
        fail("the expected repository is empty")
    try:
        with open(path, "rb") as f:
            raw = f.read(MAX_BYTES + 1)
        if len(raw) > MAX_BYTES:
            fail(f"{path} is over {MAX_BYTES} bytes -- not an evidence record")
        doc = json.loads(raw.decode("utf-8"))
    except FileNotFoundError:
        fail(f"no evidence at {path} -- did publish:image run and pass?")
    except (OSError, ValueError) as e:
        fail(f"cannot read {path}: {e}")
    if not isinstance(doc, dict):
        fail(f"{path} is not a JSON object")
    digest = doc.get("imageDigest")
    if not isinstance(digest, str) or not DIGEST.fullmatch(digest):
        fail(f"{path}: imageDigest is not sha256:<64 hex>: {digest!r}")
    want = f"{repository}@{digest}"
    if doc.get("imageReference") != want:
        fail(f"{path}: imageReference is {doc.get('imageReference')!r}, expected {want!r}")
    commit = os.environ.get("CI_COMMIT_SHA", "")
    if commit and doc.get("commit") != commit:
        fail(f"{path}: records commit {doc.get('commit')!r}, but this pipeline is {commit!r}")
    print(digest)


if __name__ == "__main__":
    main(sys.argv)
