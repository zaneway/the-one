create table if not exists mvp_acceptance_run (
  id                         text primary key,
  name                       text not null,
  mode                       text not null,
  workspace_id               text not null,
  project_id                 text,
  repo_id                    text,
  baseline_type              text not null,
  candidate_type             text not null,
  status                     text not null,
  started_at                 datetime not null,
  ended_at                   datetime,
  summary_json               text,
  report_path                text,
  created_at                 datetime not null,
  updated_at                 datetime not null
);

create index if not exists idx_mvp_acceptance_run_scope
  on mvp_acceptance_run(workspace_id, project_id, repo_id, started_at);

create index if not exists idx_mvp_acceptance_run_status
  on mvp_acceptance_run(status, updated_at);

create table if not exists mvp_acceptance_task (
  id                         text primary key,
  run_id                     text not null,
  scenario_id                text not null,
  round                      integer not null,
  agent_type                 text not null,
  baseline                   boolean not null default false,
  session_id                 text,
  task_id                    text,
  retrieval_trace_id         text,
  status                     text not null,
  task_success               boolean not null default false,
  expected_json              text,
  observed_json              text,
  failure_reason             text,
  started_at                 datetime not null,
  ended_at                   datetime,
  created_at                 datetime not null,
  updated_at                 datetime not null
);

create index if not exists idx_mvp_acceptance_task_run
  on mvp_acceptance_task(run_id, scenario_id, round, baseline);

create index if not exists idx_mvp_acceptance_task_session
  on mvp_acceptance_task(session_id, task_id);

create index if not exists idx_mvp_acceptance_task_trace
  on mvp_acceptance_task(retrieval_trace_id);

create table if not exists mvp_metric_sample (
  id                         text primary key,
  run_id                     text not null,
  scenario_id                text,
  task_result_id             text,
  agent_type                 text,
  metric_name                text not null,
  metric_value               real not null,
  numerator                  real,
  denominator                real,
  unit                       text not null,
  threshold_value            real,
  threshold_operator         text,
  passed                     boolean not null default false,
  source_json                text,
  created_at                 datetime not null
);

create unique index if not exists idx_mvp_metric_sample_unique
  on mvp_metric_sample(
    run_id,
    coalesce(scenario_id, ''),
    coalesce(task_result_id, ''),
    coalesce(agent_type, ''),
    metric_name
  );

create index if not exists idx_mvp_metric_run
  on mvp_metric_sample(run_id, metric_name, scenario_id);

create index if not exists idx_mvp_metric_agent
  on mvp_metric_sample(run_id, agent_type, metric_name);

create table if not exists mvp_agent_capability (
  id                         text primary key,
  run_id                     text not null,
  agent_type                 text not null,
  adapter_name               text,
  adapter_version            text,
  capture_level              integer not null,
  conversation_capture       boolean not null default false,
  tool_call_capture          boolean not null default false,
  tool_output_capture        boolean not null default false,
  file_edit_capture          boolean not null default false,
  session_lifecycle          boolean not null default false,
  memory_observe             boolean not null default false,
  capability_coverage        real not null default 0,
  completeness               real not null default 0,
  degradation_reasons_json   text,
  created_at                 datetime not null
);

create unique index if not exists idx_mvp_agent_capability_unique
  on mvp_agent_capability(run_id, agent_type);
