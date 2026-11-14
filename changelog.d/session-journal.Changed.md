- **Sessions are saved as small appends.** A turn used to rewrite the whole session file.
  It now appends to `<id>.journal.jsonl`, which is folded into `<id>.json` when it grows as
  large as the file, so bytes written per turn no longer grow with the conversation. New
  `session_save_interval` setting (default 0, every turn) holds saves for that long to cut
  writes further on SD and eMMC storage. Existing session files load unchanged.
