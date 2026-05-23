create table if not exists memory_item (
  id                       text primary key,
  scope                    text not null,
  workspace_id             text,
  user_id                  text,
  project_id               text,
  repo_id                  text,
  session_id               text,
  task_id                  text,

  memory_type              text not null,
  source_type              text,
  created_by               text,
  source_quality           real default 0.7,
  title                    text,
  content                  text not null,
  normalized_content       text,
  search_text              text,
  keywords_json            text,
  entities_json            text,
  retrieval_cues_json      text,
  tags_json                text,

  state                    text not null,
  confidence               real default 0.7,
  importance               real default 0.5,
  encoding_depth           integer default 2,
  decay_rate               real not null default 0.8,
  reinforcement_count      real default 0,
  effective_reinforcement  real default 0,
  retention_score          real default 0,
  tier                     text not null,

  valid_from               datetime,
  valid_until              datetime,
  created_at               datetime not null,
  updated_at               datetime not null,
  last_accessed_at         datetime,
  last_reinforced_at       datetime,
  last_validated_at        datetime,

  pinned                   boolean default false,
  user_confirmed           boolean default false,
  version                  integer default 1,
  supersedes_id            text
);

create index if not exists idx_memory_scope
  on memory_item(scope, workspace_id, project_id, repo_id);

create index if not exists idx_memory_state
  on memory_item(state, tier, updated_at);

create index if not exists idx_memory_type
  on memory_item(memory_type, state);

create table if not exists evidence (
  id                       text primary key,
  raw_event_id              text,
  source_type               text not null,
  interpreted_statement     text not null,
  keywords_json             text,
  salient_spans_json        text,
  source_ref_json           text,
  confidence                real default 0.7,
  created_at                datetime not null
);

create table if not exists memory_evidence_link (
  memory_id        text not null,
  evidence_id      text not null,
  relation_type    text not null,
  weight           real default 1.0,
  primary key (memory_id, evidence_id)
);

create table if not exists memory_review (
  id                  text primary key,
  memory_id           text not null,
  review_type         text not null,
  status              text not null,
  reviewer            text,
  feedback            text,
  original_content    text,
  edited_content      text,
  created_at          datetime not null,
  reviewed_at         datetime
);

create table if not exists review_checkpoint (
  id                            text primary key,
  memory_id                     text not null,
  workspace_id                  text,
  project_id                    text,
  repo_id                       text,
  session_id                    text,
  task_id                       text,
  checkpoint_type               text not null,
  review_intent_json            text not null,
  target_docs_json              text not null,
  target_sections_json          text,
  target_hashes_json            text,
  conclusion                    text not null,
  confirmed_baseline_json       text,
  ignored_items_json            text,
  deferred_items_json           text,
  open_items_json               text,
  next_review_policy_json       text,
  created_at                    datetime not null,
  updated_at                    datetime not null
);

create index if not exists idx_review_checkpoint_scope
  on review_checkpoint(workspace_id, project_id, repo_id, checkpoint_type, updated_at);

create table if not exists memory_tombstone (
  memory_id           text primary key,
  deleted_reason      text,
  deleted_by          text,
  content_hash        text,
  deleted_at          datetime not null
);
