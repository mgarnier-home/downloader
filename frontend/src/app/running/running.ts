import { Component, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ApiService } from '../api/api.service';
import { JobView, Kind, QueueFile } from '../api/models';
import { apiError, formatBytes, formatEta, formatSpeed, KIND_CLASS, KIND_LABEL } from '../format';
import { HistoryPage } from '../history/history';
import { VideosPage } from '../videos/videos';
import { commonPath, episodeCells, fileMeta, filePercent, hosts, jobPercent, summarize } from './group';

const FILTERS: { label: string; kind: Kind | '' }[] = [
  { label: 'Tout', kind: '' }, { label: 'Séries', kind: 'series' }, { label: 'Films', kind: 'movie' },
  { label: 'Animés', kind: 'anime' }, { label: 'Musique', kind: 'music' },
];

@Component({
  selector: 'app-running',
  imports: [RouterLink, VideosPage, HistoryPage],
  templateUrl: './running.html',
  styleUrl: './running.css',
})
export class RunningPage {
  private readonly api = inject(ApiService);
  protected readonly filters = FILTERS;
  protected readonly kindLabel = KIND_LABEL;
  protected readonly kindClass = KIND_CLASS;
  protected readonly formatBytes = formatBytes;
  protected readonly filePercent = filePercent;
  protected readonly jobPercent = jobPercent;
  protected readonly episodeCells = episodeCells;
  protected readonly commonPath = commonPath;
  protected readonly hosts = hosts;

  protected readonly queue = this.api.queue();
  protected readonly kind = signal<Kind | ''>('');
  protected readonly message = signal('');
  protected readonly replacing = signal<number | null>(null);

  private readonly view = computed(() => (this.queue.hasValue() ? this.queue.value() : null));
  protected readonly summary = computed(() => {
    const v = this.view();
    return v ? summarize(v) : null;
  });
  protected readonly paused = computed(() => this.view()?.paused ?? false);
  protected readonly eta = computed(() => {
    const s = this.summary();
    return s ? formatEta(s.bytesLeft, s.speed) : '';
  });

  private readonly jobs = computed(() => {
    const k = this.kind();
    return (this.view()?.jobs ?? []).filter((j) => !k || j.kind === k);
  });
  protected readonly errors = computed(() =>
    this.jobs().flatMap((job) => job.files.filter((f) => f.state === 'error').map((file) => ({ job, file }))));
  protected readonly running = computed(() =>
    this.jobs().filter((j) => j.files.some((f) => f.state === 'running' || f.state === 'pending')));

  protected count(job: JobView, state: QueueFile['state']) {
    return job.files.filter((f) => f.state === state).length;
  }

  /** Texte de droite d'une ligne : épisodes actifs, titres rangés ou état du fichier unique. */
  protected meta(job: JobView): string {
    const running = job.files.filter((f) => f.state === 'running');
    const pending = this.count(job, 'pending');
    if (job.kind === 'music') return `${this.count(job, 'done')} / ${job.files.length} titres`;
    if (job.files.length === 1) return fileMeta(job.files[0]);
    const parts = running.map((f) =>
      `${episodeCells({ ...job, files: [f] })[0].label} ${filePercent(f)} %${f.speed > 0 ? ` (${formatSpeed(f.speed)})` : ''}`);
    if (pending) parts.push(`${pending} en attente`);
    return parts.join(' · ');
  }

  protected idle(job: JobView) {
    return !job.files.some((f) => f.state === 'running');
  }

  protected async act(action: () => Promise<unknown>) {
    this.message.set('');
    try {
      await action();
      this.replacing.set(null);
    } catch (e) {
      this.message.set(apiError(e, "L'action a échoué."));
    }
    this.queue.reload();
  }

  protected togglePause() {
    return this.act(() => (this.paused() ? this.api.resume() : this.api.pause()));
  }
  protected retry(f: QueueFile) { return this.act(() => this.api.retry(f.id)); }
  protected remove(f: QueueFile) { return this.act(() => this.api.remove(f.id)); }
  protected replace(f: QueueFile, input: string) {
    return input.trim() ? this.act(() => this.api.replace(f.id, input.trim())) : undefined;
  }
}
