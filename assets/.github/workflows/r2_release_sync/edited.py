import tempfile
from pathlib import Path

from common import asset_key, asset_matches_r2, delete_key, event_release, index_entry, list_keys, release_json_key
from common import release_metadata, r2_upload_json, tag_prefix, download_asset, upload_asset
from index import upsert_release


def main() -> int:
    release = event_release()
    tag = release["tag_name"]
    assets = [asset for asset in release.get("assets", []) if asset.get("name")]
    desired_keys = {asset_key(tag, asset) for asset in assets}

    existing_keys = list_keys(tag_prefix(tag))
    for key in sorted(existing_keys - desired_keys):
        if key == release_json_key(tag):
            continue
        delete_key(key)

    with tempfile.TemporaryDirectory() as tmp:
        tmpdir = Path(tmp)
        for asset in assets:
            if asset_matches_r2(tag, asset):
                print(f"Unchanged asset, skipped: {asset['name']}")
                continue
            local_file = tmpdir / asset["name"]
            download_asset(asset, local_file)
            upload_asset(tag, asset, local_file)

    r2_upload_json(release_json_key(tag), release_metadata(release))
    upsert_release(index_entry(release))
    print(f"Edited {tag} in R2.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
