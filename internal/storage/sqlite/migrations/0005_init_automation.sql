create table if not exists async_job (
  id                  text primary key,
  job_type            text not null,
  target_type         text not null,
  target_id           text not null,
  status              text not null,
  priority            integer not null default 5,
  retry_count         integer not null default 0,
  max_retries         integer not null default 3,
  next_run_at         datetime not null,
  last_error          text,
  dedup_key           text,
  payload_json        text,
  created_at          datetime not null,
  updated_at          datetime not null
);

create index if not exists idx_async_job_poll
  on async_job(status, next_run_at, priority, created_at);

create index if not exists idx_async_job_target
  on async_job(target_type, target_id, job_type);

create unique index if not exists idx_async_job_dedup
  on async_job(dedup_key)
  where dedup_key is not null and dedup_key != '';

create table if not exists memory_candidate (
  id                         text primary key,
  raw_event_id                text,
  evidence_id                 text,
  provider                    text not null,
  memory_type                 text not null,
  scope                       text not null,
  workspace_id                text,
  user_id                     text,
  project_id                  text,
  repo_id                     text,
  session_id                  text,
  task_id                     text,
  title                       text,
  content                     text not null,
  keywords_json               text,
  entities_json               text,
  retrieval_cues_json         text,
  tags_json                   text,
  source_evidence_ids_json    text,
  review_checkpoint_json      text,
  confidence                  real not null default 0.7,
  importance                  real not null default 0.5,
  encoding_depth              integer not null default 2,
  candidate_reason_json       text,
  admission_score             real,
  admission_decision          text,
  admission_reason_json       text,
  resulting_memory_id         text,
  status                      text not null,
  dedup_key                   text,
  created_at                  datetime not null,
  updated_at                  datetime not null
);

create index if not exists idx_memory_candidate_source
  on memory_candidate(raw_event_id, evidence_id, provider);

create index if not exists idx_memory_candidate_status
  on memory_candidate(status, created_at);

create index if not exists idx_memory_candidate_scope
  on memory_candidate(workspace_id, project_id, repo_id, memory_type, status);

create unique index if not exists idx_memory_candidate_dedup
  on memory_candidate(dedup_key)
  where dedup_key is not null and dedup_key != '';

create unique index if not exists idx_evidence_auto_dedup
  on evidence(raw_event_id, source_type, interpreted_statement)
  where raw_event_id is not null and raw_event_id != '';

create table if not exists memory_relation (
  id               text primary key,
  source_id        text not null,
  target_id        text not null,
  relation_type    text not null,
  weight           real not null default 1.0,
  created_at       datetime not null,
  updated_at       datetime not null
);

create index if not exists idx_memory_relation_source
  on memory_relation(source_id, relation_type);

create index if not exists idx_memory_relation_target
  on memory_relation(target_id, relation_type);

create unique index if not exists idx_memory_relation_unique_edge
  on memory_relation(source_id, target_id, relation_type);
