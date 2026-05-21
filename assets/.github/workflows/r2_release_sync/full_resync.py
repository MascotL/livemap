import os
import tempfile
import urllib.parse
import urllib.request
from pathlib import Path

from common import asset_key, asset_matches_r2, delete_key, delete_prefix, download_asset, release_json_key
from common import release_metadata, r2_upload_json, tag_prefix, upload_asset, list_keys, required_env
from index import load_index, recalculate_latest


API_ROOT = "https://api.github.com"


def github_request(path: str):
    import json
    import urllib.error

    token = required_env("GH_TOKEN")
    url = path if path.startswith("https://") else f"{API_ROOT}{path}"
    request = urllib.request.Request(
        url,
        headers={
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {token}",
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "r2-release-sync",
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        if exc.code == 404:
            return None
        body = exc.read().decode("utf-8", errors="replace")
        raise SystemExit(f"GitHub API failed: {exc.code} {url}\n{body}") from exc


def repo() -> str:
    return required_env("GITHUB_REPOSITORY")


def fetch_release(tag: str) -> dict | None:
    quoted = urllib.parse.quote(tag, safe="")
    release = github_request(f"/repos/{repo()}/releases/tags/{quoted}")
    if release and release.get("draft"):
        return None
    return release


def fetch_releases() -> list[dict]:
    releases = []
    page = 1
    while True:
        batch = github_request(f"/repos/{repo()}/releases?per_page=100&page={page}")
        if not batch:
            break
        releases.extend(item for item in batch if not item.get("draft", False))
        page += 1
    releases.sort(key=lambda item: item.get("published_at") or item.get("created_at") or "", reverse=True)
    return releases


def index_entry(release: dict) -> dict:
    tag = release["tag_name"]
    assets = release.get("assets", [])
    return {
        "tag": tag,
        "name": release.get("name") or tag,
        "prerelease": bool(release.get("prerelease", False)),
        "published_at": release.get("published_at"),
        "created_at": release.get("created_at"),
        "html_url": release.get("html_url"),
        "release_path": f"{tag}/release.json",
        "asset_count": len(assets),
        "total_size": sum(int(asset.get("size") or 0) for asset in assets),
    }


def sync_release_assets(release: dict, delete_extra: bool) -> None:
    tag = release["tag_name"]
    assets = [asset for asset in release.get("assets", []) if asset.get("name")]
    desired_keys = {asset_key(tag, asset) for asset in assets}

    if delete_extra:
        for key in sorted(list_keys(tag_prefix(tag)) - desired_keys):
            if key == release_json_key(tag):
                continue
            delete_key(key)

    with tempfile.TemporaryDirectory() as tmp:
        tmpdir = Path(tmp)
        for asset in assets:
            if asset_matches_r2(tag, asset):
                print(f"Unchanged asset, skipped: {tag}/{asset['name']}")
                continue
            local_file = tmpdir / asset["name"]
            download_asset(asset, local_file)
            upload_asset(tag, asset, local_file)

    r2_upload_json(release_json_key(tag), release_metadata(release))


def r2_tag_prefixes() -> set[str]:
    from common import release_prefix

    prefixes = set()
    root = f"{release_prefix()}/"
    for key in list_keys(root):
        rest = key[len(root) :]
        if "/" in rest:
            prefixes.add(rest.split("/", 1)[0])
    return prefixes


def sync_single(tag: str, delete_extra: bool) -> None:
    release = fetch_release(tag)
    if not release:
        if delete_extra:
            delete_prefix(tag_prefix(tag))
            data = load_index()
            data["releases"] = [entry for entry in data.get("releases", []) if entry.get("tag") != tag]
            recalculate_latest(data)
            from common import index_key

            r2_upload_json(index_key(), data)
            print(f"Release {tag} does not exist; removed from R2.")
        else:
            print(f"Release {tag} does not exist; delete_extra=false, leaving R2 unchanged.")
        return

    sync_release_assets(release, delete_extra)

    data = load_index()
    data["releases"] = [entry for entry in data.get("releases", []) if entry.get("tag") != tag]
    data["releases"].append(index_entry(release))
    recalculate_latest(data)
    from common import index_key

    r2_upload_json(index_key(), data)
    print(f"Resynced release {tag}.")


def sync_all(delete_extra: bool) -> None:
    releases = fetch_releases()
    github_tags = {release["tag_name"] for release in releases}

    for release in releases:
        sync_release_assets(release, delete_extra)

    if delete_extra:
        for tag in sorted(r2_tag_prefixes() - github_tags):
            delete_prefix(tag_prefix(tag))

    data = load_index()
    if delete_extra:
        data["releases"] = [index_entry(release) for release in releases]
    else:
        entries_by_tag = {entry.get("tag"): entry for entry in data.get("releases", []) if entry.get("tag")}
        for release in releases:
            entries_by_tag[release["tag_name"]] = index_entry(release)
        data["releases"] = list(entries_by_tag.values())

    recalculate_latest(data)
    from common import index_key

    r2_upload_json(index_key(), data)
    print(f"Resynced {len(releases)} release(s). delete_extra={delete_extra}")


def main() -> int:
    tag = os.environ.get("RESYNC_TAG", "").strip()
    delete_extra = required_env("DELETE_EXTRA").lower() == "true"

    if tag:
        sync_single(tag, delete_extra)
    else:
        sync_all(delete_extra)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
