import { Component, inject, signal } from '@angular/core';
import { ApiService } from '../api/api.service';
import { HistoryFilter } from '../api/models';
import { formatBytes, KIND_CLASS, packSummary } from '../format';

const FILTERS: { value: HistoryFilter; label: string }[] = [
  { value: 'all', label: 'Tout' }, { value: 'video', label: 'Vidéos' },
  { value: 'music', label: 'Musique' }, { value: 'error', label: 'Erreurs' },
];
const KIND_NAME: Record<string, string> = { series: 'Série', movie: 'Film', anime: 'Animé', music: 'Musique' };

@Component({
  selector: 'app-history',
  templateUrl: './history.html',
  styleUrl: './history.css',
})
export class HistoryPage {
  protected readonly filters = FILTERS;
  protected readonly kindName = KIND_NAME;
  protected readonly kindClass = KIND_CLASS;
  protected readonly formatBytes = formatBytes;
  protected readonly packSummary = packSummary;
  protected readonly filter = signal<HistoryFilter>('all');
  protected readonly history = inject(ApiService).history(this.filter);

  /** « 31/10 14:02 » ; tiret quand la date est absente (0001-01-01). */
  protected when(iso: string): string {
    const d = new Date(iso);
    if (d.getFullYear() < 2000) return '—';
    const day = d.toLocaleDateString('fr-FR', { day: '2-digit', month: '2-digit' });
    return `${day} ${d.toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit' })}`;
  }
}
