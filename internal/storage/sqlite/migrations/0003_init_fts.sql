create virtual table if not exists memory_item_fts
using fts5(
  memory_id unindexed,
  search_text,
  tokenize = 'unicode61'
);
