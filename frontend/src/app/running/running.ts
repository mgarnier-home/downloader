import { Component, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ApiService } from '../api/api.service';
import { Kind, QueueFile } from '../api/models';
import { apiError, formatBytes, formatEta, KIND_CLASS, KIND_LABEL } from '../format';
import { HistoryPage } from '../history/history';
import { VideosPage } from '../videos/videos';
import { activeFiles, fileLabel, fileMeta, filePercent, summarize } from './group';

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
  protected readonly fileMeta = fileMeta;
  protected readonly fileLabel = fileLabel;

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
  protected readonly running = computed(() => activeFiles(this.jobs()));

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
