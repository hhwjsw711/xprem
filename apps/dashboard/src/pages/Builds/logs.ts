import stripAnsi from 'strip-ansi';

export interface BuildLogEvent {
  time: string;
  level: number;
  msg: string;
  buildStepId: string;
  buildStepDisplayName: string;
  marker?: 'START_STEP' | 'END_STEP';
  result?: 'success' | 'failed' | 'warning' | 'skipped';
  durationMs?: number;
}

export interface BuildLogGroup {
  id: string;
  label: string;
  output: string;
  startedAt?: number;
  result?: BuildLogEvent['result'];
  durationMs?: number;
}

export interface BuildLogChunk {
  offset: number;
  content: string;
  createdAt: string;
}

export function appendBuildLogs(groups: Map<string, BuildLogGroup>, chunks: BuildLogChunk[]): void {
  for (const chunk of chunks) {
    for (const line of chunk.content.trimEnd().split('\n')) {
      const event: BuildLogEvent = JSON.parse(line);
      const id = event.buildStepId;
      const group = {
        ...(groups.get(id) ?? { id, label: event.buildStepDisplayName, output: '' }),
      };
      if (event.marker === 'START_STEP') group.startedAt = Date.parse(event.time);
      else if (event.marker === 'END_STEP') {
        group.result = event.result;
        group.durationMs = event.durationMs;
      } else group.output += `${event.msg}\n`;
      groups.set(id, group);
    }
  }
}

export function buildLogsText(groups: Iterable<BuildLogGroup>): string {
  return [...groups].map(group => `${group.label}\n${stripAnsi(group.output)}`).join('\n');
}
