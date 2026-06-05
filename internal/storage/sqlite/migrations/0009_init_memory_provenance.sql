create table if not exists memory_provenance (
  id                  text primary key,
  memory_id           text not null,
  raw_event_id         text,
  evidence_id          text,
  candidate_id         text,

  agent_type           text,
  source_channel       text,
  source_producer      text,
  hook_name            text,
  hook_phase           text,
  event_type           text,
  capture_method       text,

  pipeline             text,
  provider             text,
  derivation_stage     text,
  admission_decision   text,
  admission_score      real,

  trace_json           text,
  created_at           datetime not null
);

create index if not exists idx_memory_provenance_memory
  on memory_provenance(memory_id, created_at);

create index if not exists idx_memory_provenance_raw_event
  on memory_provenance(raw_event_id, evidence_id, candidate_id);

create index if not exists idx_memory_provenance_source
  on memory_provenance(source_producer, hook_phase, event_type, created_at);
