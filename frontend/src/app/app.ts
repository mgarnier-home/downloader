import { Component, computed, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { ApiService } from './api/api.service';
import { formatBytes } from './format';

const dateFr = new Intl.DateTimeFormat('fr-FR', { day: 'numeric', month: 'long', year: 'numeric' });

const DOWN = { cls: 'bad', text: 'indisponible', short: 'erreur' };
// Les erreurs du backend commencent déjà par le nom du service.
const clean = (err: string, service: string) =>
  err.replace(new RegExp(`^${service}\\s*:\\s*`), '') || 'indisponible';

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
    { path: '/music', label: 'Musique', exact: false },
  ];

  /** Fichiers actifs ou en erreur (les « en attente » ne comptent pas). 0 si la file est illisible. */
  protected readonly activeCount = computed(() => {
    const q = this.api.queue();
    // value() lève en état d'erreur : on garde hasValue().
    if (!q.hasValue()) return 0;
    return q.value().jobs.flatMap((j) => j.files).filter((f) => f.state === 'running' || f.state === 'error').length;
  });

  private readonly status = computed(() => {
    const s = this.api.status();
    return s.hasValue() ? s.value() : null;
  });
  private readonly statusDown = computed(() => this.api.status().error() !== undefined);

  protected readonly alldebrid = computed(() => {
    const s = this.status()?.alldebrid;
    if (!s) return this.statusDown() ? DOWN : null;
    if (!s.ok) return { cls: 'bad', text: clean(s.error, 'AllDebrid'), short: 'erreur' };
    const until = s.until ? new Date(`${s.until}T00:00:00`) : null;
    const text = !s.premium ? 'non premium' : until ? `premium jusqu'au ${dateFr.format(until)}` : 'premium';
    return { cls: s.premium ? '' : 'warn', text, short: 'OK' };
  });

  protected readonly deezer = computed(() => {
    const s = this.status()?.deezer;
    if (!s) return this.statusDown() ? DOWN : null;
    return s.ok ? { cls: '', text: 'connecté', short: 'OK' } : { cls: 'bad', text: clean(s.error, 'Deezer'), short: 'erreur' };
  });

  protected readonly free = computed(() => {
    const s = this.status();
    return s ? `${formatBytes(s.freeBytes)} libres` : null;
  });
}
