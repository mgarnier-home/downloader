// Types alignés sur le contrat JSON du backend (api/types.go).
export type Kind = 'series' | 'movie' | 'anime' | 'music';
export type FileState = 'pending' | 'running' | 'done' | 'error';

export interface Status {
  alldebrid: { ok: boolean; premium: boolean; until: string; error: string };
  deezer: { ok: boolean; error: string };
  freeBytes: number;
}

export interface TMDBResult {
  id: number;
  title: string;
  year: number;
  animation: boolean;
  poster: string;
  tv: boolean;
}

export interface AnalyzeRow {
  input: string;
  name: string;
  size: number;
  online: boolean;
  error: string;
  kind: Kind | '';
  tmdb: TMDBResult | null;
  season: number;
  episode: number;
  needsReview: boolean;
  dest: string;
  exists: boolean;
}

export interface DeezerArtist { id: number; name: string; picture: string }
export interface DeezerAlbum {
  id: number; title: string; artist: string; cover: string; year: number; tracks: number; present: boolean;
}
export interface DeezerTrack {
  id: number; title: string; artist: string; album: string; albumId: number; duration: number;
}
export interface DeezerResults {
  artists: DeezerArtist[];
  albums: DeezerAlbum[];
  tracks: DeezerTrack[];
}

export interface EnqueueFile {
  input: string;
  name: string;
  size: number;
  dest: string;
  overwrite: boolean;
}
export interface EnqueueRequest {
  kind: Kind;
  title: string;
  ref: string;
  files: EnqueueFile[];
  deezer: boolean;
}

export interface QueueFile {
  id: number;
  name: string;
  dest: string;
  state: FileState;
  error: string;
  bytes: number;
  total: number;
  speed: number;
  attempts: number;
}
export interface JobView {
  id: number;
  kind: Kind;
  title: string;
  files: QueueFile[];
}
export interface QueueView {
  paused: boolean;
  speed: number;
  jobs: JobView[];
}

export type HistoryFilter = 'all' | 'video' | 'music' | 'error';
export interface HistoryEntry {
  finishedAt: string;
  title: string;
  name: string;
  dest: string;
  kind: Kind;
  size: number;
  state: FileState;
  error: string;
}
