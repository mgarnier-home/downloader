import { Component, computed, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { ApiService } from './api/api.service';
import { formatBytes } from './format';

const dateFr = new Intl.DateTimeFormat('fr-FR', { day: 'numeric', month: 'long', year: 'numeric' });

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, RouterLink, RouterLinkActive],
  templateUrl: './app.html',
  styleUrl: './app.css',
})
export class App {
  private readonly api = inject(ApiService);
  protected readonly tabs = [
    { path: '/', label: 'En cours', exact: true },
    { path: '/videos', label: 'Vidéos', exact: false },
    { path: '/music', label: 'Musique', exact: false },
    { path: '/history', label: 'Historique', exact: false },
  ];

  /** Fichiers actifs ou en erreur (les « en attente » ne comptent pas). */
  protected readonly activeCount = computed(
    () =>
      this.api
        .queue()
        .value()
        ?.jobs.flatMap((j) => j.files)
        .filter((f) => f.state === 'running' || f.state === 'error').length ?? 0,
  );

  protected readonly alldebrid = computed(() => {
    const s = this.api.status().value()?.alldebrid;
    if (!s) return null;
    if (!s.ok) return { cls: 'bad', text: s.error || 'indisponible' };
    const until = s.until ? new Date(`${s.until}T00:00:00`) : null;
    const text = !s.premium ? 'non premium' : until ? `premium jusqu'au ${dateFr.format(until)}` : 'premium';
    return { cls: s.premium ? '' : 'warn', text };
  });

  protected readonly deezer = computed(() => {
    const s = this.api.status().value()?.deezer;
    if (!s) return null;
    return s.ok ? { cls: '', text: 'connecté' } : { cls: 'bad', text: s.error || 'indisponible' };
  });

  protected readonly free = computed(() => {
    const s = this.api.status().value();
    return s ? `${formatBytes(s.freeBytes)} libres` : null;
  });
}
