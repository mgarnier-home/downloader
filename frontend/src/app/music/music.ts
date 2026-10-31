import { Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { ApiService } from '../api/api.service';
import { DeezerResults } from '../api/models';
import { apiError } from '../format';

type Filter = 'all' | 'artists' | 'albums' | 'tracks';
const FILTERS: { value: Filter; label: string }[] = [
  { value: 'all', label: 'Tout' }, { value: 'artists', label: 'Artistes' },
  { value: 'albums', label: 'Albums' }, { value: 'tracks', label: 'Titres' },
];
const LINK = /deezer\.com\/(?:[a-z]{2}(?:-[a-z]{2})?\/)?(album|track)\/(\d+)/i;

/** Lien Deezer collé : sa référence (« deezer:album:1 »), seule envoyée au serveur. */
export function deezerLink(q: string): { type: string; ref: string } | null {
  const m = LINK.exec(q);
  return m ? { type: m[1].toLowerCase(), ref: `deezer:${m[1].toLowerCase()}:${m[2]}` } : null;
}

@Component({
  selector: 'app-music',
  templateUrl: './music.html',
  styleUrl: './music.css',
})
export class MusicPage {
  private readonly api = inject(ApiService);
  private timer: ReturnType<typeof setTimeout> | undefined;
  protected readonly filters = FILTERS;
  protected readonly query = signal('');
  protected readonly filter = signal<Filter>('all');
  protected readonly results = signal<DeezerResults | null>(null);
  protected readonly busy = signal(false);
  protected readonly error = signal('');
  /** Résultat de chaque ajout, par référence (« deezer:album:1 »). */
  protected readonly added = signal<Record<string, string>>({});
  /** Échecs d'ajout (le bouton reste utilisable pour réessayer). */
  protected readonly failed = signal<Record<string, string>>({});

  /** Lien Deezer collé : proposé directement, sans recherche. */
  protected readonly link = computed(() => deezerLink(this.query()));
  protected readonly empty = computed(() => {
    const r = this.results();
    return r !== null && !r.artists.length && !r.albums.length && !r.tracks.length;
  });

  constructor() {
    inject(DestroyRef).onDestroy(() => clearTimeout(this.timer));
  }

  protected type(q: string) {
    this.query.set(q);
    clearTimeout(this.timer);
    if (!q.trim() || this.link()) {
      this.results.set(null);
      this.error.set('');
      return;
    }
    this.timer = setTimeout(() => this.search(q.trim()), 300);
  }

  private async search(q: string) {
    this.busy.set(true);
    this.error.set('');
    try {
      const r = await this.api.deezerSearch(q);
      if (q === this.query().trim()) this.results.set(r); // une frappe plus récente l'emporte
    } catch (e) {
      this.error.set(apiError(e, 'La recherche Deezer a échoué.'));
    }
    this.busy.set(false);
  }

  protected showArtist(name: string) {
    this.filter.set('albums');
    this.type(name);
  }

  protected async add(ref: string) {
    const set = (v: string) => this.added.update((a) => ({ ...a, [ref]: v }));
    if (this.added()[ref] === '…') return;
    this.failed.update((f) => ({ ...f, [ref]: '' }));
    set('…');
    try {
      await this.api.enqueue({ kind: 'music', title: '', ref: '', files: [], deezer: ref });
      set('Ajouté');
    } catch (e) {
      const dup = (e as { status?: number }).status === 409;
      this.added.update((a) => ({ ...a, [ref]: dup ? 'Déjà dans la file' : '' }));
      if (!dup) this.failed.update((f) => ({ ...f, [ref]: apiError(e, "L'ajout a échoué.") }));
    }
  }

  protected locked(ref: string) { return !!this.added()[ref]; }

  protected duration(s: number) {
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
  }
  protected show(part: 'artists' | 'albums' | 'tracks') {
    return this.filter() === 'all' || this.filter() === part;
  }
}
