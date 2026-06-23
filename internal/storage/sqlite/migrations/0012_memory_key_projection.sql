create table if not exists memory_key (
  key_id                   text primary key,
  memory_id                text not null,
  key_type                 text not null,
  key_text                 text not null,
  key_hash                 text not null,
  weight                   real not null default 1.0,
  scope                    text not null,
  memory_type              text not null,
  state                    text not null,
  tier                     text not null,
  created_at               datetime not null,
  updated_at               datetime not null
);

create unique index if not exists idx_memory_key_unique
  on memory_key(memory_id, key_type, key_hash);

create index if not exists idx_memory_key_lookup
  on memory_key(scope, memory_type, state, key_type, key_hash);

create index if not exists idx_memory_key_memory
  on memory_key(memory_id);
