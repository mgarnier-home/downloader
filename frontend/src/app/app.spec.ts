import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { App } from './app';
import { routes } from './app.routes';

const okStatus = {
  alldebrid: { ok: true, premium: true, until: '2027-03-05', error: '' },
  deezer: { ok: true, error: '' },
  freeBytes: 4_100_000_000_000,
};
const queue = {
  paused: false,
  speed: 0,
  jobs: [{ id: 1, kind: 'movie', title: 'x', files: [
    { id: 1, state: 'running' }, { id: 2, state: 'error' }, { id: 3, state: 'pending' },
  ] }],
};

async function setup(status: object | null, statusFails: boolean, queueBody: object | null, queueFails: boolean) {
  TestBed.configureTestingModule({
    providers: [provideHttpClient(), provideHttpClientTesting(), provideRouter(routes)],
  });
  const fixture = TestBed.createComponent(App);
  const http = TestBed.inject(HttpTestingController);
  TestBed.tick(); // lance les requêtes des ressources
  const answer = (url: string, body: object | null, fails: boolean) => {
    const req = http.expectOne(url);
    if (fails) req.flush({ error: 'boom' }, { status: 500, statusText: 'Erreur' });
    else req.flush(body);
  };
  answer('/api/status', status, statusFails);
  answer('/api/queue', queueBody, queueFails);
  await fixture.whenStable();
  fixture.detectChanges();
  const el = fixture.nativeElement as HTMLElement;
  return { el, text: el.textContent ?? '' };
}

describe('App', () => {
  it('affiche les pastilles et le compteur', async () => {
    const { el, text } = await setup(okStatus, false, queue, false);
    expect(text).toContain("premium jusqu'au 5 mars 2027");
    expect(text).toContain('4,1 To libres');
    expect(el.querySelector('.tabs em')?.textContent).toBe('2');
  });

  it('retire le préfixe du service des erreurs', async () => {
    const { el, text } = await setup(
      { ...okStatus, alldebrid: { ok: false, premium: false, until: '', error: 'AllDebrid : clé invalide' },
        deezer: { ok: false, error: 'Deezer : arl expiré' } },
      false, queue, false,
    );
    expect(text).toContain('clé invalide');
    expect(text).not.toContain('AllDebrid : ');
    expect(text).not.toContain('Deezer : ');
    expect(el.querySelector('.chip')?.getAttribute('title')).toBe('AllDebrid : clé invalide');
  });

  it("affiche « indisponible » quand /api/status répond 500", async () => {
    const { el, text } = await setup(null, true, queue, false);
    expect(text).toContain('indisponible');
    expect(el.querySelectorAll('.tabs a').length).toBe(4);
  });

  it('survit à une erreur de /api/queue', async () => {
    const { el } = await setup(okStatus, false, null, true);
    expect(el.querySelectorAll('.tabs a').length).toBe(4);
    expect(el.querySelector('.tabs em')).toBeNull();
  });
});
