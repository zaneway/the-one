create table if not exists retrieval_trace (
  id                  text primary key,
  session_id          text,
  task_id             text,
  workspace_id        text,
  project_id          text,
  repo_id             text,
  query               text,
  task                text,
  retrieval_intent    text,
  retrieval_mode      text,
  used_fts            boolean not null default false,
  used_vector         boolean not null default false,
  used_relation       boolean not null default false,
  used_code_index     boolean not null default false,
  used_doc_index      boolean not null default false,
  fallback_reason     text,
  candidate_count     integer not null default 0,
  injected_count      integer not null default 0,
  latency_ms          integer,
  status              text not null default 'completed',
  created_at          datetime not null
);

create index if not exists idx_retrieval_trace_scope
  on retrieval_trace(workspace_id, project_id, repo_id, created_at);

create index if not exists idx_retrieval_trace_task
  on retrieval_trace(session_id, task_id, created_at);

create table if not exists memory_access_log (
  id                    text primary key,
  memory_id             text not null,
  session_id            text,
  task_id               text,
  retrieval_trace_id    text,
  event_type            text not null,
  event_weight          real not null,
  source_type           text,
  source_quality        real not null default 0.7,
  query                 text,
  rank                  integer,
  score                 real,
  score_breakdown_json  text,
  inclusion_reason_json text,
  used_in_context       boolean not null default false,
  feedback              text,
  created_at            datetime not null
);

create index if not exists idx_memory_access_log_memory
  on memory_access_log(memory_id, created_at);

create index if not exists idx_memory_access_log_trace
  on memory_access_log(retrieval_trace_id, rank);

create index if not exists idx_memory_access_log_task
  on memory_access_log(session_id, task_id, created_at);

create table if not exists code_ref (
  id                  text primary key,
  memory_id           text not null,
  repo_id             text not null,
  commit_hash         text,
  file_path           text,
  symbol              text,
  line_start          integer,
  line_end            integer,
  content_hash        text,
  ref_summary         text,
  resolve_status      text not null default 'unresolved',
  resolved_at         datetime,
  created_at          datetime not null,
  updated_at          datetime not null
);

create index if not exists idx_code_ref_memory
  on code_ref(memory_id);

create index if not exists idx_code_ref_repo_file
  on code_ref(repo_id, file_path, symbol);

create index if not exists idx_code_ref_status
  on code_ref(resolve_status, updated_at);

create table if not exists memory_embedding (
  memory_id           text not null,
  embedding_model     text not null,
  embedding_dim       integer not null,
  embedding           blob not null,
  created_at          datetime not null,
  updated_at          datetime not null,
  primary key(memory_id, embedding_model)
);

create index if not exists idx_memory_embedding_model
  on memory_embedding(embedding_model, updated_at);

create table if not exists doc_snapshot (
  id                  text primary key,
  workspace_id        text not null,
  project_id          text not null default '',
  repo_id             text not null default '',
  doc_path            text not null,
  doc_role            text,
  content_hash        text not null,
  modified_at         datetime,
  section_count       integer not null default 0,
  created_at          datetime not null
);

create index if not exists idx_doc_snapshot_scope
  on doc_snapshot(workspace_id, project_id, repo_id, doc_path, created_at);

create unique index if not exists idx_doc_snapshot_dedup
  on doc_snapshot(workspace_id, project_id, repo_id, doc_path, content_hash);

create table if not exists doc_section_snapshot (
  id                  text primary key,
  snapshot_id         text not null,
  section_id          text not null,
  heading_path_json   text,
  level               integer,
  start_line          integer,
  end_line            integer,
  content_hash        text not null,
  summary             text,
  created_at          datetime not null
);

create index if not exists idx_doc_section_snapshot
  on doc_section_snapshot(snapshot_id, section_id);
