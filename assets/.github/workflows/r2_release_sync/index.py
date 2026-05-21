from common import index_key, r2_get_json, r2_upload_json


EMPTY_INDEX = {
    "schema_version": 1,
    "latest": None,
    "latest_stable": None,
    "latest_prerelease": None,
    "releases": [],
}


def load_index() -> dict:
    data = r2_get_json(index_key(), EMPTY_INDEX.copy())
    if not isinstance(data, dict):
        return EMPTY_INDEX.copy()
    data.setdefault("schema_version", 1)
    data.setdefault("latest", None)
    data.setdefault("latest_stable", None)
    data.setdefault("latest_prerelease", None)
    data.setdefault("releases", [])
    return data


def sort_key(entry: dict) -> str:
    return entry.get("published_at") or entry.get("created_at") or ""


def latest_ref(entry: dict | None) -> dict | None:
    if not entry:
        return None
    return {
        "tag": entry["tag"],
        "path": entry["release_path"],
    }


def recalculate_latest(data: dict) -> None:
    releases = sorted(data.get("releases", []), key=sort_key, reverse=True)
    data["releases"] = releases
    data["latest"] = latest_ref(releases[0] if releases else None)
    stable = next((entry for entry in releases if not entry.get("prerelease")), None)
    prerelease = next((entry for entry in releases if entry.get("prerelease")), None)
    data["latest_stable"] = latest_ref(stable)
    data["latest_prerelease"] = latest_ref(prerelease)


def upsert_release(entry: dict) -> None:
    data = load_index()
    releases = [item for item in data.get("releases", []) if item.get("tag") != entry["tag"]]
    releases.append(entry)
    data["releases"] = releases
    recalculate_latest(data)
    r2_upload_json(index_key(), data)


def remove_release(tag: str) -> None:
    data = load_index()
    data["releases"] = [item for item in data.get("releases", []) if item.get("tag") != tag]
    recalculate_latest(data)
    r2_upload_json(index_key(), data)
