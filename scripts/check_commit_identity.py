#!/usr/bin/env python3
"""Validate Git commit authorship and co-author trailers for a GitHub pull request."""
from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request

EXPECTED_NAME = "Mojtaba Arvin"
EXPECTED_EMAIL = "m.arvin@live.com"
AUTOMATION_LABEL = "allow-automation-commits"
MAINTAINER_MARKER = "<!-- maintainer-directed-ai-work -->"
APPROVED_AUTOMATION_LOGINS = {"github-actions[bot]", "dependabot[bot]"}
BOT_MARKERS = (
    "[bot]", "ai agent", "ai-agent", "agent bot", "agent assistant",
    "copilot", "claude", "cursor", "windsurf", "codex", "chatgpt", "gemini", "openai", "gpt", "ai assistant", "assistant bot",
)


def is_bot_identity(name: str, email: str, login: str | None, account_type: str | None) -> bool:
    if account_type and account_type.lower() == "bot":
        return True
    if login and login.lower().endswith("[bot]"):
        return True
    identity = (name + " " + email + " " + (login or "")).lower()
    return any(marker in identity for marker in BOT_MARKERS)


def validate_payload(pr: dict, commits: list[dict]) -> list[str]:
    errors: list[str] = []
    labels = {item.get("name", "") for item in pr.get("labels", [])}
    allow_automation = AUTOMATION_LABEL in labels
    maintainer_work = MAINTAINER_MARKER in (pr.get("body") or "")

    for item in commits:
        commit = item.get("commit", {})
        author = commit.get("author") or {}
        github_author = item.get("author") or {}
        name = author.get("name") or ""
        email = author.get("email") or ""
        login = github_author.get("login")
        account_type = github_author.get("type")
        message = commit.get("message") or ""
        short_sha = (item.get("sha") or "unknown")[:12]

        if not name or not email:
            errors.append(f"{short_sha}: missing Git author name or email")
        bot_author = is_bot_identity(name, email, login, account_type)
        if bot_author:
            approved = (
                allow_automation
                and login in APPROVED_AUTOMATION_LOGINS
                and account_type == "Bot"
            )
            if not approved:
                errors.append(
                    f"{short_sha}: non-human/bot author rejected: "
                    f"{name!r} <{email}> login={login!r}"
                )

        if maintainer_work and (name != EXPECTED_NAME or email.lower() != EXPECTED_EMAIL.lower()):
            errors.append(
                f"{short_sha}: maintainer-directed AI work must use "
                f"{EXPECTED_NAME} <{EXPECTED_EMAIL}>; found {name!r} <{email}>"
            )

        for line in message.splitlines():
            if line.lower().startswith("co-authored-by:"):
                identity = line.split(":", 1)[1].strip()
                if is_bot_identity(identity, "", None, None):
                    errors.append(f"{short_sha}: bot/AI Co-authored-by trailer rejected: {identity!r}")

    return errors


def request_json(url: str, token: str) -> object:
    request = urllib.request.Request(
        url,
        headers={
            "Accept": "application/vnd.github+json",
            "Authorization": "Bearer " + token,
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "commit-identity-policy-check",
        },
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.loads(response.read().decode("utf-8"))


def fetch_all_commits(repository: str, pr_number: int, token: str) -> list[dict]:
    result: list[dict] = []
    page = 1
    while True:
        url = (
            f"https://api.github.com/repos/{repository}/pulls/{pr_number}/commits"
            f"?per_page=100&page={page}"
        )
        batch = request_json(url, token)
        if not isinstance(batch, list):
            raise RuntimeError("GitHub API returned a non-list PR commit response")
        result.extend(batch)
        if len(batch) < 100:
            return result
        page += 1


def run_self_tests() -> None:
    def item(name: str, email: str, login: str | None, account_type: str | None,
             message: str = "feat: test") -> dict:
        return {
            "sha": "a" * 40,
            "commit": {"author": {"name": name, "email": email}, "message": message},
            "author": ({"login": login, "type": account_type} if login else None),
        }

    assert not validate_payload({"body": "", "labels": []}, [
        item(EXPECTED_NAME, EXPECTED_EMAIL, "mojtaba-arvin", "User")
    ])
    assert not validate_payload({"body": "", "labels": []}, [
        item("External Contributor", "dev@example.org", "contributor", "User")
    ])
    assert validate_payload({"body": "", "labels": []}, [
        item("github-actions[bot]", "bot@users.noreply.github.com", "github-actions[bot]", "Bot")
    ])
    assert validate_payload({"body": "", "labels": []}, [
        item("Human", "human@example.org", "human", "User",
             "feat: test\n\nCo-authored-by: Copilot <copilot[bot]@users.noreply.github.com>")
    ])
    assert not validate_payload({"body": MAINTAINER_MARKER, "labels": []}, [
        item(EXPECTED_NAME, EXPECTED_EMAIL, "mojtaba-arvin", "User")
    ])
    assert validate_payload({"body": MAINTAINER_MARKER, "labels": []}, [
        item("Different Human", "human@example.org", "human", "User")
    ])
    assert not validate_payload({"body": "", "labels": [{"name": AUTOMATION_LABEL}]}, [
        item("github-actions[bot]", "bot@users.noreply.github.com", "github-actions[bot]", "Bot")
    ])
    print("PASS: commit identity policy self-tests")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", default=os.environ.get("GITHUB_REPOSITORY"))
    parser.add_argument("--pr-number", type=int, default=int(os.environ.get("PR_NUMBER", "0")))
    parser.add_argument("--token", default=os.environ.get("GITHUB_TOKEN"))
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()

    if args.self_test:
        run_self_tests()
    if not args.repository or not args.pr_number or not args.token:
        if args.self_test and not args.repository and not args.pr_number and not args.token:
            return 0
        parser.error("--repository, --pr-number, and --token are required for PR validation")

    base = "https://api.github.com/repos/" + args.repository
    pr = request_json(base + "/pulls/" + str(args.pr_number), args.token)
    commits = fetch_all_commits(args.repository, args.pr_number, args.token)
    if not commits:
        print("ERROR: no PR commits were returned; refusing to pass an empty result")
        return 2

    errors = validate_payload(pr, commits)
    print(
        f"Checked {len(commits)} commits in {args.repository}#{args.pr_number}; "
        f"maintainer-directed AI marker={'yes' if MAINTAINER_MARKER in (pr.get('body') or '') else 'no'}."
    )
    if errors:
        for error in errors:
            print("ERROR: " + error)
        return 1
    print("PASS: no prohibited bot/agent authors or co-author trailers found")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except urllib.error.HTTPError as exc:
        print(f"ERROR: GitHub API HTTP {exc.code}: {exc.reason}", file=sys.stderr)
        raise SystemExit(2)
    except (urllib.error.URLError, TimeoutError, ValueError, RuntimeError) as exc:
        print(f"ERROR: identity validation could not complete: {exc}", file=sys.stderr)
        raise SystemExit(2)
