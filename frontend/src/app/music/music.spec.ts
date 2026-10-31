import { deezerLink } from './music';

describe('deezerLink', () => {
  it('donne la référence envoyée au serveur, pas le lien tel que collé', () => {
    expect(deezerLink('  https://www.deezer.com/fr/album/302127?utm=x  ')).toEqual({ type: 'album', ref: 'deezer:album:302127' });
    expect(deezerLink('https://deezer.com/TRACK/3135556')).toEqual({ type: 'track', ref: 'deezer:track:3135556' });
  });
  it('ignore une recherche', () => {
    expect(deezerLink('daft punk')).toBeNull();
  });
});
