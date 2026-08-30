export interface Source {
  id: number;
  name: string;
  path: string;
  kind: string;
  provider_type: string;
  enabled: boolean;
  file_count: number;
  created_at: string;
}

export interface ParseResult {
  title_candidates?: string[];
  ep_number_raw?: string;
  episode?: number;
  episode_end?: number;
  ep_type_hint?: string;
  release_group?: string;
  version_key?: string;
  subtitle_lang?: string;
  batch?: boolean;
  confidence?: number;
  rule?: string;
}

export interface SourceFile {
  id: number;
  abs_path: string;
  kind: string;
  status: string;
  size: number;
  mtime: string;
  parse_result?: ParseResult;
}

export interface ReviewItem {
  case_id: number;
  placement_id: number;
  reason: string;
  state: string;
  created_at: string;
  file_path: string;
  series_id: number;
  series_title: string;
  slot_type: string;
  season?: number;
  episode?: number;
  episode_end?: number;
  version_key?: string;
  confidence: number;
  decision_source: string;
}

export interface Task {
  id: number;
  kind: string;
  state: string;
  payload?: string;
  progress?: Record<string, unknown>;
  error?: string;
  created_at: string;
}

export interface IndexStatus {
  dump_version: string;
  imported_at: string;
  source_url: string;
  subjects: number;
  episodes: number;
  relations: number;
  task_id: number;
}

export interface Me {
  username: string;
  using_default_credentials: boolean;
  version: string;
}
