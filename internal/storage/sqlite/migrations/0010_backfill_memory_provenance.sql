update memory_provenance
set
  raw_event_id = coalesce(nullif(memory_provenance.raw_event_id, ''), r.id),
  agent_type = coalesce(nullif(memory_provenance.agent_type, ''), r.agent_type),
  source_channel = coalesce(nullif(memory_provenance.source_channel, ''), r.source_channel),
  event_type = coalesce(nullif(memory_provenance.event_type, ''), r.event_type),
  capture_method = coalesce(nullif(memory_provenance.capture_method, ''), 'adapter_hook'),
  hook_phase = case
    when coalesce(memory_provenance.hook_phase, '') not in ('', 'unknown') then memory_provenance.hook_phase
    when r.source_channel = 'mcp_tool' then 'manual_observe'
    when r.event_type = 'session.start' then 'session_start'
    when r.event_type = 'session.end' then 'session_end'
    when r.event_type = 'file.edit.summary' then 'file_edit'
    when r.event_type = 'tool.result.summary' then 'post_tool'
    when r.event_type = 'agent.response.summary' then 'turn_end'
    else 'unknown'
  end,
  source_producer = case
    when coalesce(memory_provenance.source_producer, '') != '' then memory_provenance.source_producer
    when r.source_channel = 'mcp_tool' then 'mcp:memory_observe'
    when r.event_type = 'session.start' then r.agent_type || '_hook:SessionStart'
    when r.event_type = 'session.end' then r.agent_type || '_hook:SessionEnd'
    when r.event_type = 'agent.response.summary' and r.agent_type = 'cursor' then 'cursor_hook:afterAgentResponse'
    when r.event_type = 'agent.response.summary' then r.agent_type || '_hook:Stop'
    when r.event_type = 'tool.result.summary' and r.agent_type = 'cursor' then 'cursor_hook:afterMCPExecution'
    when r.event_type = 'tool.result.summary' then r.agent_type || '_hook:PostToolUse'
    when r.event_type = 'file.edit.summary' and r.agent_type = 'cursor' then 'cursor_hook:afterFileEdit'
    when r.event_type = 'file.edit.summary' then r.agent_type || '_hook:PostToolUse'
    else memory_provenance.source_producer
  end
from raw_event as r
where r.id = coalesce(
    nullif(memory_provenance.raw_event_id, ''),
    (select e.raw_event_id from evidence as e where e.id = memory_provenance.evidence_id limit 1)
  )
  and (
    coalesce(memory_provenance.source_producer, '') = ''
    or coalesce(memory_provenance.hook_phase, '') in ('', 'unknown')
    or coalesce(memory_provenance.agent_type, '') = ''
    or coalesce(memory_provenance.source_channel, '') = ''
  );
