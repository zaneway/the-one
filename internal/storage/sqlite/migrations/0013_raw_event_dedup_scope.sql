drop index if exists idx_raw_event_dedup_session;

create unique index if not exists idx_raw_event_dedup_session_source_scope
  on raw_event(
    content_hash,
    session_id,
    event_type,
    coalesce(source_channel, ''),
    coalesce(workspace_id, ''),
    coalesce(project_id, ''),
    coalesce(repo_id, '')
  )
  where content_hash is not null and content_hash != '' and session_id is not null;
