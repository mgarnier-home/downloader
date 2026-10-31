import { HttpClient, httpResource } from '@angular/common/http';
import { DestroyRef, inject, Injectable, Signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import {
  AnalyzeRow, DeezerResults, EnqueueRequest, HistoryEntry, JobView, QueueView, Status, TMDBResult,
} from './models';

@Injectable({ providedIn: 'root' })
export class ApiService {
  private readonly http = inject(HttpClient);
  private readonly statusRes = httpResource<Status>(() => '/api/status');
  private readonly queueRes = httpResource<QueueView>(() => '/api/queue');

  constructor() {
    // La file bouge sans cesse : relue toutes les 2 s tant que l'appli est ouverte.
    const timer = setInterval(() => this.queueRes.reload(), 2000);
    inject(DestroyRef).onDestroy(() => clearInterval(timer));
  }

  status() { return this.statusRes; }
  queue() { return this.queueRes; }

  history(filter: Signal<string>) {
    return httpResource<HistoryEntry[]>(() => ({ url: '/api/history', params: { filter: filter() } }));
  }

  analyze(links: string[]) {
    return firstValueFrom(this.http.post<AnalyzeRow[]>('/api/videos/analyze', { links }));
  }
  enqueue(req: EnqueueRequest) {
    return firstValueFrom(this.http.post<JobView>('/api/queue', req));
  }
  retry(id: number) { return firstValueFrom(this.http.post<void>(`/api/files/${id}/retry`, null)); }
  replace(id: number, input: string) {
    return firstValueFrom(this.http.post<void>(`/api/files/${id}/replace`, { input }));
  }
  remove(id: number) { return firstValueFrom(this.http.delete<void>(`/api/files/${id}`)); }
  pause() { return firstValueFrom(this.http.post<void>('/api/pause', null)); }
  resume() { return firstValueFrom(this.http.post<void>('/api/resume', null)); }

  tmdbSearch(q: string, tv: boolean, year?: number) {
    const params: Record<string, string> = { q, tv: String(tv) };
    if (year) params['year'] = String(year);
    return firstValueFrom(this.http.get<TMDBResult[]>('/api/tmdb/search', { params }));
  }
  deezerSearch(q: string) {
    return firstValueFrom(this.http.get<DeezerResults>('/api/deezer/search', { params: { q } }));
  }
}
