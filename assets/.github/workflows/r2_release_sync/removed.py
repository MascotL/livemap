from common import delete_prefix, event_release, tag_prefix
from index import remove_release


def main() -> int:
    release = event_release()
    tag = release["tag_name"]
    delete_prefix(tag_prefix(tag))
    remove_release(tag)
    print(f"Removed {tag} from R2.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
