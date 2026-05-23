create table if not exists agent_session (
  id                         text primary key,
  agent_type                 text not null,
  workspace_id               text not null,
  project_id                 text,
  repo_id                    text,
  capture_level              integer not null default 1,
  capture_capabilities_json  text,
  capture_quality_json       text,
  started_at                 datetime not null,
  ended_at                   datetime,
  goal_summary               text,
  status                     text not null,
  created_at                 datetime not null,
  updated_at                 datetime not null
);

create index if not exists idx_agent_session_scope
  on agent_session(workspace_id, project_id, repo_id, agent_type, started_at);

create index if not exists idx_agent_session_status
  on agent_session(status, updated_at);

create table if not exists agent_task (
  id                 text primary key,
  session_id         text,
  workspace_id       text not null,
  project_id         text,
  repo_id            text,
  task_summary       text not null,
  status             text not null,
  started_at         datetime not null,
  ended_at           datetime,
  outcome_summary    text,
  created_at         datetime not null,
  updated_at         datetime not null
);

create index if not exists idx_agent_task_session
  on agent_task(session_id, status, started_at);

create index if not exists idx_agent_task_scope
  on agent_task(workspace_id, project_id, repo_id, status, started_at);

create table if not exists raw_event (
  id                  text primary key,
  session_id          text,
  task_id             text,
  workspace_id        text,
  project_id          text,
  repo_id             text,
  agent_type          text,
  event_type          text not null,
  source_channel      text,
  occurred_at         datetime not null,

  actor               text,
  tool_name           text,
  input_summary       text,
  output_summary      text,
  content_summary     text,

  keywords_json       text,
  salient_spans_json  text,
  source_refs_json    text,
  content_hash        text,

  sensitivity         text default 'normal',
  retention_hint      text,
  created_at          datetime not null
);

create index if not exists idx_raw_event_session
  on raw_event(session_id, task_id, event_type, occurred_at);

create index if not exists idx_raw_event_scope
  on raw_event(workspace_id, project_id, repo_id, event_type, occurred_at);

create index if not exists idx_raw_event_hash
  on raw_event(content_hash, session_id, event_type);

create index if not exists idx_raw_event_agent
  on raw_event(agent_type, source_channel, occurred_at);

create unique index if not exists idx_raw_event_dedup_session
  on raw_event(content_hash, session_id, event_type)
  where content_hash is not null and content_hash != '' and session_id is not null;
