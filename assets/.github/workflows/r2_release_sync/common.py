import json
import mimetypes
import os
import subprocess
import tempfile
import urllib.parse
import urllib.request
from pathlib import Path


def required_env(name: str) -> str:
    value = os.environ.get(name)
    if not value:
        raise SystemExit(f"Missing required environment variable: {name}")
    return value


def bucket() -> str:
    return required_env("R2_BUCKET")


def release_prefix() -> str:
    return required_env("R2_RELEASE_PREFIX").strip("/")


def download_base_url() -> str:
    return required_env("DOWNLOAD_BASE_URL").rstrip("/")


def event_payload() -> dict:
    path = Path(required_env("GITHUB_EVENT_PATH"))
    return json.loads(path.read_text(encoding="utf-8"))


def event_release() -> dict:
    release = event_payload().get("release")
    if not isinstance(release, dict):
        raise SystemExit("GitHub event payload does not contain a release object.")
    if not release.get("tag_name"):
        raise SystemExit("GitHub release payload does not contain tag_name.")
    return release


def action_name() -> str:
    return event_payload().get("action", "")


def tag_prefix(tag: str) -> str:
    return f"{release_prefix()}/{tag}/"


def index_key() -> str:
    return f"{release_prefix()}/index.json"


def release_json_key(tag: str) -> str:
    return f"{tag_prefix(tag)}release.json"


def run_aws(args: list[str], check: bool = True) -> subprocess.CompletedProcess:
    command = ["aws", *args, "--endpoint-url", required_env("R2_ENDPOINT")]
    return subprocess.run(command, check=check, text=True, capture_output=True)


def r2_head(key: str) -> dict | None:
    result = run_aws(["s3api", "head-object", "--bucket", bucket(), "--key", key], check=False)
    if result.returncode != 0:
        return None
    return json.loads(result.stdout)


def r2_get_json(key: str, default):
    with tempfile.TemporaryDirectory() as tmp:
        path = Path(tmp) / "object.json"
        result = run_aws(["s3", "cp", f"s3://{bucket()}/{key}", str(path)], check=False)
        if result.returncode != 0:
            return default
        return json.loads(path.read_text(encoding="utf-8"))


def r2_upload_json(key: str, data: dict) -> None:
    with tempfile.TemporaryDirectory() as tmp:
        path = Path(tmp) / "object.json"
        path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        run_aws(
            [
                "s3",
                "cp",
                str(path),
                f"s3://{bucket()}/{key}",
                "--content-type",
                "application/json",
                "--cache-control",
                "no-cache",
            ]
        )


def list_keys(prefix: str) -> set[str]:
    keys = set()
    continuation = None
    while True:
        args = ["s3api", "list-objects-v2", "--bucket", bucket(), "--prefix", prefix]
        if continuation:
            args.extend(["--continuation-token", continuation])
        result = run_aws(args)
        payload = json.loads(result.stdout)
        keys.update(item["Key"] for item in payload.get("Contents", []))
        if not payload.get("IsTruncated"):
            return keys
        continuation = payload.get("NextContinuationToken")


def delete_key(key: str) -> None:
    print(f"Deleting R2 object: {key}")
    run_aws(["s3api", "delete-object", "--bucket", bucket(), "--key", key])


def delete_prefix(prefix: str) -> None:
    run_aws(["s3", "rm", f"s3://{bucket()}/{prefix}", "--recursive"], check=False)


def normalize_metadata(metadata: dict | None) -> dict:
    if not metadata:
        return {}
    return {str(key).lower(): str(value) for key, value in metadata.items()}


def asset_digest(asset: dict) -> str:
    return asset.get("digest") or ""


def asset_sha256(asset: dict) -> str:
    digest = asset_digest(asset)
    if digest.startswith("sha256:"):
        return digest.split(":", 1)[1]
    return ""


def asset_key(tag: str, asset: dict) -> str:
    return f"{tag_prefix(tag)}{asset['name']}"


def content_type(asset: dict, local_file: Path) -> str:
    if asset.get("content_type"):
        return asset["content_type"]
    guessed, _ = mimetypes.guess_type(local_file.name)
    return guessed or "application/octet-stream"


def download_asset(asset: dict, destination: Path) -> None:
    url = asset.get("browser_download_url")
    if not url:
        raise SystemExit(f"Release asset {asset.get('name')} does not have browser_download_url.")

    headers = {
        "Accept": "application/octet-stream",
        "User-Agent": "r2-release-sync",
    }
    token = os.environ.get("GH_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"

    request = urllib.request.Request(url, headers=headers)
    with urllib.request.urlopen(request, timeout=120) as response:
        destination.write_bytes(response.read())


def upload_asset(tag: str, asset: dict, local_file: Path) -> None:
    digest = asset_digest(asset)
    sha256 = asset_sha256(asset)
    metadata = [
        f"github-digest={digest}",
        f"github-asset-id={asset.get('id') or ''}",
        f"github-asset-name={asset.get('name') or ''}",
    ]
    if sha256:
        metadata.append(f"sha256={sha256}")

    key = asset_key(tag, asset)
    print(f"Uploading asset: {key}")
    run_aws(
        [
            "s3",
            "cp",
            str(local_file),
            f"s3://{bucket()}/{key}",
            "--content-type",
            content_type(asset, local_file),
            "--metadata",
            ",".join(metadata),
        ]
    )


def asset_matches_r2(tag: str, asset: dict) -> bool:
    head = r2_head(asset_key(tag, asset))
    if not head:
        return False

    metadata = normalize_metadata(head.get("Metadata"))
    expected_digest = asset_digest(asset)
    remote_digest = metadata.get("github-digest")
    expected_size = int(asset.get("size") or 0)
    remote_size = int(head.get("ContentLength", -1))

    if expected_digest:
        return remote_digest == expected_digest and remote_size == expected_size
    return expected_size > 0 and remote_size == expected_size


def clean_join_url(base: str, *parts: str) -> str:
    quoted = [urllib.parse.quote(part.strip("/"), safe="-._~") for part in parts]
    return "/".join([base.rstrip("/"), *quoted])


def release_metadata(release: dict) -> dict:
    tag = release["tag_name"]
    assets = []
    for asset in release.get("assets", []):
        name = asset.get("name") or ""
        assets.append(
            {
                "name": name,
                "size": asset.get("size", 0),
                "content_type": asset.get("content_type") or "application/octet-stream",
                "github_asset_id": asset.get("id"),
                "github_download_url": asset.get("browser_download_url"),
                "r2_key": f"{tag_prefix(tag)}{name}",
                "relative_path": f"{tag}/{name}",
                "download_url": clean_join_url(download_base_url(), tag, name),
                "created_at": asset.get("created_at"),
                "updated_at": asset.get("updated_at"),
                "download_count": asset.get("download_count", 0),
                "digest": asset_digest(asset),
                "sha256": asset_sha256(asset) or None,
            }
        )

    return {
        "schema_version": 1,
        "tag": tag,
        "name": release.get("name") or tag,
        "body": release.get("body") or "",
        "html_url": release.get("html_url"),
        "draft": bool(release.get("draft", False)),
        "prerelease": bool(release.get("prerelease", False)),
        "created_at": release.get("created_at"),
        "published_at": release.get("published_at"),
        "target_commitish": release.get("target_commitish"),
        "r2_prefix": tag_prefix(tag).rstrip("/"),
        "asset_count": len(assets),
        "total_size": sum(asset["size"] for asset in assets),
        "assets": assets,
    }


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
