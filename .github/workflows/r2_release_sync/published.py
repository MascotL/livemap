import tempfile
from pathlib import Path

from common import delete_prefix, event_release, index_entry, release_json_key, release_metadata, r2_upload_json, tag_prefix
from common import download_asset, upload_asset
from index import upsert_release


def main() -> int:
    release = event_release()
    tag = release["tag_name"]

    # A published release is treated as a fresh version for this tag.
    delete_prefix(tag_prefix(tag))

    with tempfile.TemporaryDirectory() as tmp:
        tmpdir = Path(tmp)
        for asset in release.get("assets", []):
            name = asset.get("name")
            if not name:
                continue
            local_file = tmpdir / name
            download_asset(asset, local_file)
            upload_asset(tag, asset, local_file)

    r2_upload_json(release_json_key(tag), release_metadata(release))
    upsert_release(index_entry(release))
    print(f"Published {tag} to R2.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
