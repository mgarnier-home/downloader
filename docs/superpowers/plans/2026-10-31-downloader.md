# Downloader v0.1.0 — plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal :** une application web qui télécharge vidéos (AllDebrid, HTTP direct) et musique (Deezer) et
les range dans la médiathèque au format Jellyfin / Navidrome.

**Architecture :** un binaire Go sert `/api/` et le front Angular embarqué (`embed`). Les sources
(`alldebrid`, `http`, `deezer`) produisent des `source.Item`, que `queue` télécharge dans
`download/` puis déplace à leur destination, calculée par `naming`. L'état vit dans SQLite (`store`).

**Tech Stack :** Go 1.26.4 (bibliothèque standard, `modernc.org/sqlite`, `golang.org/x/crypto/blowfish`,
`github.com/bogem/id3v2/v2`, `github.com/mgarnier-home/utils` v1.4.0 : `config.GetConfig`,
`logger`, `version.Handler`, `buildinfo`), Angular 22 (standalone,
signals, `httpResource`), Node 24.16.0, golangci-lint 2.15.0, Task, Docker multi-étapes.

**Spec :** `docs/superpowers/specs/2026-10-31-downloader-design.md`. Maquettes :
`docs/maquettes/maquettes.html` (onglets « B · Bureau » et « B · Mobile »).

## Contraintes globales

- Documentation, commentaires et messages d'erreur en français ; code (fichiers, identifiants) en
  anglais. Commits au format Conventional Commits, sur `release/v0.1.0`.
- Module Go : `github.com/mgarnier-home/downloader/backend`. Build `CGO_ENABLED=0`.
- Aucune dépendance en dehors de celles listées dans « Tech Stack » sans accord du propriétaire.
  Avant d'écrire un utilitaire générique, regarder s'il existe dans `github.com/mgarnier-home/utils`
  (configuration, journalisation, `/version`, infos de build) et l'utiliser.
- Les docs, commentaires et messages du dépôt ne citent aucun autre projet ni aucune infrastructure
  particulière.
- Le dépôt est public : aucun secret, aucune IP, aucun nom d'hôte interne dans le code, les tests ou
  la doc.
- Variables : `DOWNLOADER_SERVER_PORT` (défaut `8080`), `DOWNLOADER_MEDIA_DIR` (défaut `/media`),
  `DOWNLOADER_DATA_DIR` (défaut `/data`), `DOWNLOADER_ALLDEBRID_API_KEY`, `DOWNLOADER_DEEZER_ARL`,
  `DOWNLOADER_TMDB_API_KEY` (les trois requises).
- Les secrets ne sont jamais renvoyés par l'API ni écrits dans les logs.
- SQLite : `journal_mode=DELETE`, `busy_timeout=5000`, `SetMaxOpenConns(1)`, pas de WAL.
- Parallélisme : 3 vidéos et 5 morceaux au maximum. Nouvelles tentatives : 3, après 10 s, 30 s, 90 s.
- Musique : MP3 128 kbps (`MP3_128`), pochette intégrée 800 px, `cover.jpg` 1200 px.
- Toute destination passe par `naming.Within` avant écriture.
- Front : pas de bibliothèque de composants UI ; thème sombre unique ; IBM Plex Sans / IBM Plex Mono.
  Couleurs de la maquette : fond `#1e1f29`, panneau `#232530`, carte `#282a36`, bordure `#343746`,
  surélevé `#383a4a`, texte `#f8f8f2`, atténué `#a4acc4`, accent `#bd93f9`, ok `#50fa7b`,
  alerte `#ffb86c`, erreur `#ff5555`, info `#8be9fd`.
- Définition de terminé (avant chaque push) : `cd backend && go vet ./... && go test -race ./... &&
  golangci-lint run`, puis `cd frontend && npx ng lint && npx ng build && npx ng test --watch=false`,
  et la même commande `ng test` avec `TZ=UTC`.

## Points de vigilance

Cas que la spec implique sans les nommer ; chacun a son test dans la tâche indiquée.

1. **Le même lien ou la même destination ajouté deux fois** (double collage, analyse relancée) : le
   second est refusé avec « déjà dans la file », pas téléchargé deux fois. → Tâche 9.
2. **Un nom saisi à la main qui contient `/`, `..` ou un chemin absolu** : refusé en 400, rien n'est
   écrit. → Tâche 9 (et `naming.Within`, tâche 3).
3. **Reprise d'un `.part` alors que le serveur ignore `Range`** (réponse 200 au lieu de 206) : le
   fichier repart de zéro, il n'est pas concaténé. → Tâche 8.
4. **Un morceau Deezer indisponible au sein d'un album** (pas de source `MP3_128`) : ce morceau passe
   en erreur, les autres continuent. → Tâche 7.
5. **Aucune fiche TMDB trouvée** : la ligne garde un nom tiré du nom de fichier (PascalCase) et porte
   « TMDB : aucune fiche », elle n'est pas perdue. → Tâche 9.

---

## Structure des fichiers

```
.tool-versions  .gitignore  taskfile.yml  Dockerfile
.github/workflows/test.yml  .github/workflows/publish.yml
backend/
  go.mod  .golangci.yml
  main.go          câblage, routes, front embarqué
  config.go        loadConfig(getenv)
  spa.go           spaHandler(fs.FS)
  web/browser/.gitkeep
  naming/parse.go  ParseFilename
  naming/paths.go  PascalCase, SafeName, VideoPath, BonusPath, MusicPath, Within
  naming/index.go  Index (tmdbid → dossier existant)
  store/store.go   Open, schéma, requêtes jobs/files
  tmdb/tmdb.go     Client (recherche TV / film, genres)
  source/source.go Source, Item, Track
  source/http.go   HTTP direct
  source/alldebrid.go
  deezer/crypt.go  TrackKey, Decrypter
  deezer/client.go API publique + passerelle privée + get_url
  deezer/tags.go   WriteTags
  source/deezer.go
  queue/queue.go   Manager : planification, pause, instantané
  queue/download.go transfert, reprise, déplacement
  api/types.go     structs JSON
  api/handlers.go  Handlers, Register
frontend/          Angular (src/app/api, shell, running, videos, music, history)
```

---

### Tâche 1 : socle backend, CI

**Files :**
- Create : `.tool-versions`, `.gitignore`, `taskfile.yml`, `.github/workflows/test.yml`,
  `backend/go.mod`, `backend/.golangci.yml`, `backend/main.go`, `backend/config.go`,
  `backend/spa.go`, `backend/web/browser/.gitkeep`
- Test : `backend/config_test.go`

**Interfaces :**
- Produces :
  ```go
  type Config struct {
      Port         string `key:"DOWNLOADER_SERVER_PORT" default-value:"8080"`
      MediaDir     string `key:"DOWNLOADER_MEDIA_DIR" default-value:"/media"`
      DataDir      string `key:"DOWNLOADER_DATA_DIR" default-value:"/data"`
      AllDebridKey string `key:"DOWNLOADER_ALLDEBRID_API_KEY" required:"true"`
      DeezerARL    string `key:"DOWNLOADER_DEEZER_ARL" required:"true"`
      TMDBKey      string `key:"DOWNLOADER_TMDB_API_KEY" required:"true"`
  }
  func loadConfig() (Config, error) // utils config.GetConfig, erreurs jointes (errors.Join), puis contrôle du port 1–65535
  func spaHandler(fsys fs.FS) http.Handler // sert le fichier demandé s'il existe, sinon index.html
  ```
  `GET /version` via `github.com/mgarnier-home/utils/version.Handler()`. Journalisation :
  `logger.InitAppLogger("downloader")` puis `logger.Infof` / `Warnf` / `Errorf` de utils.

Contenu des fichiers de socle :
- `.tool-versions` : `golang 1.26.4`, `nodejs 24.16.0`, `golangci-lint 2.15.0`.
- `.gitignore` : `frontend/node_modules/`, `frontend/.angular/`, `frontend/dist/`,
  `backend/web/browser/*` sauf `.gitkeep`, `/bin/`, `.env`, `*.db`.
- `.env.example` : les six variables, secrets vides (`config.GetConfig` lit `.env` en local).
- `backend/.golangci.yml` (format v2) : linters `funlen` (60 lignes, 40 instructions), `gocognit`
  (15), `nestif` (4), `unparam` ; formateur `gofmt`.
- `taskfile.yml` : `build:front` (`npm ci` si besoin, `npx ng build`, recopie de
  `frontend/dist/downloader/browser/` dans `backend/web/browser/` en gardant `.gitkeep`),
  `build:back` (`CGO_ENABLED=0 go build -o ../bin/downloader .`), `build` (les deux), `test`
  (Définition de terminé).
- `.github/workflows/test.yml` : sur PR vers `main` et push sur `main`, annulation des exécutions
  obsolètes, `permissions: contents: read`. Job Back (`ubuntu-latest`, `actions/setup-go` avec
  `go-version-file: backend/go.mod`) : `go vet ./...`, `go test -race ./...`,
  `golangci/golangci-lint-action` en v2.15.0. Job Front (`actions/setup-node` avec
  `node-version-file: .tool-versions`, cache npm) : `npm ci`, `npx ng lint`, `npx ng build`,
  `npx ng test --watch=false`. Ce job n'a rien à faire tant que `frontend/` n'existe pas : il est
  ajouté à la tâche 10.

- [ ] **Step 1 : tests de configuration**

```go
// variables posées avec t.Setenv
func TestLoadConfigDefaults(t *testing.T)        // les 3 secrets fournis → Port "8080", MediaDir "/media", DataDir "/data"
func TestLoadConfigMissingSecret(t *testing.T)   // DOWNLOADER_DEEZER_ARL vide → erreur contenant "DOWNLOADER_DEEZER_ARL"
func TestLoadConfigBadPort(t *testing.T)         // DOWNLOADER_SERVER_PORT "70000" → erreur
```

- [ ] **Step 2 :** `cd backend && go test ./...` → FAIL (`loadConfig` indéfini).
- [ ] **Step 3 :** écrire `config.go`, `spa.go`, `main.go` (charge la config, logger utils,
  `http.NewServeMux`, `GET /version`, `/` → `spaHandler` sur `//go:embed all:web/browser`,
  `ListenAndServe`).
- [ ] **Step 4 :** `go test ./... && go vet ./... && golangci-lint run` → PASS.
- [ ] **Step 5 :** commit `chore: socle backend et CI`.

### Tâche 2 : lecture des noms de fichier

**Files :** Create `backend/naming/parse.go` ; Test `backend/naming/parse_test.go`

**Interfaces :**
- Produces : `type Parsed struct { Title string; Year int; Season, Episode int; HasEpisode bool; Ext string }` ;
  `func ParseFilename(name string) Parsed`. `Ext` inclut le point (`.mkv`). `Season` vaut aussi
  quand seul `Sxx` est présent (bonus) ; `HasEpisode` n'est vrai qu'avec un épisode.

Approche : retirer l'extension ; chercher, dans l'ordre, `S(\d{1,2})[ ._-]?E(\d{1,3})`, `(\d{1,2})x(\d{2})`,
puis `S(\d{1,2})` seul (insensible à la casse) ; l'année est le **dernier** jeton `(19|20)\d{2}`
délimité par des séparateurs, compris entre 1900 et l'année courante + 1, et qui n'est pas le premier
jeton du nom (`1917.2019…` → titre 1917, année 2019) ; le titre est ce qui précède le premier élément trouvé (ou, à défaut,
la première balise de release connue : `MULTi`, `VFF`, `VOSTFR`, `FRENCH`, `2160p`, `1080p`, `720p`,
`WEB`, `BluRay`, `HDR`), points et `_` remplacés par des espaces, espaces réduits.

- [ ] **Step 1 : test en table**

| Entrée | Title | Year | S | E | HasEpisode | Ext |
| --- | --- | --- | --- | --- | --- | --- |
| `The.Boys.S01E04.MULTi.HDR.2160p.WEB.DTS-HD.MA.5.1.H265-Wawacity.red.mkv` | The Boys | 0 | 1 | 4 | oui | .mkv |
| `Dunkirk.2017.MULTi.1080p.BluRay.x264-LOST.mkv` | Dunkirk | 2017 | 0 | 0 | non | .mkv |
| `Arcane.S02.E05.VOSTFR.1080p.mp4` | Arcane | 0 | 2 | 5 | oui | .mp4 |
| `breaking_bad_3x07_720p.avi` | breaking bad | 0 | 3 | 7 | oui | .avi |
| `Arcane.S02.Bonus.Making.Of.MULTi.mkv` | Arcane | 0 | 2 | 0 | non | .mkv |
| `Spirit.Untamed.2021.MULTi.1080p.WEB.mkv` | Spirit Untamed | 2021 | 0 | 0 | non | .mkv |
| `1917.2019.MULTi.2160p.mkv` | 1917 | 2019 | 0 | 0 | non | .mkv |

(La dernière ligne vérifie qu'un titre numérique n'est pas pris pour une année quand une vraie année suit.)

- [ ] **Step 2 :** `go test ./naming/` → FAIL.
- [ ] **Step 3 :** implémenter `ParseFilename`.
- [ ] **Step 4 :** `go test ./naming/` → PASS.
- [ ] **Step 5 :** commit `feat: lecture des noms de fichier vidéo`.

### Tâche 3 : noms et chemins

**Files :** Create `backend/naming/paths.go` ; Test `backend/naming/paths_test.go`

**Interfaces :**
- Produces :
  - `type Kind string` ; constantes `Series Kind = "series"`, `Movie = "movie"`, `Anime = "anime"`, `Music = "music"`.
  - `func PascalCase(title string) string`
  - `func SafeName(s string) string` — remplace `/ \ : * ? " < > |` par `_`, retire espaces de bord.
  - `func VideoPath(kind Kind, name string, season, episode int, ext string) string` — chemin relatif.
  - `func BonusPath(name string, season int, original string) string` — `series/<name>/S<ss>/Bonus/<original>`.
  - `type TrackInfo struct { AlbumArtist, Artist, Album, Title string; Track, Disc, DiskCount int }`
  - `func MusicPath(t TrackInfo) string` — chemin relatif, extension `.mp3`.
  - `func Within(root, rel string) (string, error)` — renvoie le chemin absolu nettoyé, erreur si
    `rel` est absolu, vide, ou si le résultat sort de `root`.

- [ ] **Step 1 : tests**

```go
// PascalCase
"Spirit : l'indomptable" → "SpiritLIndomptable"
"Dune : Première partie" → "DunePremierePartie"
"Les Minions 2 : Il était une fois Gru" → "LesMinions2IlEtaitUneFoisGru"
"the boys" → "TheBoys"
// VideoPath
VideoPath(Series, "TheBoys", 2, 1, ".mkv") == "series/TheBoys/S02/TheBoys.S02.E01.mkv"
VideoPath(Movie, "Dunkerque", 0, 0, ".mkv") == "films/Dunkerque/Dunkerque.mkv"
VideoPath(Anime, "SpiritLIndomptable", 0, 0, ".mp4") == "animes/SpiritLIndomptable/SpiritLIndomptable.mp4"
BonusPath("Arcane", 2, "Arcane Bonus Making Of.mkv") == "series/Arcane/S02/Bonus/Arcane Bonus Making Of.mkv"
// MusicPath
{AlbumArtist:"Gojira",Artist:"Gojira",Album:"Fortitude",Title:"Born For One Thing",Track:1,Disc:1,DiskCount:1}
  → "musics/Gojira/Fortitude/Gojira - Fortitude - 01 - Born For One Thing.mp3"
même piste avec Disc:2, DiskCount:2 → "musics/Gojira/Fortitude/CD2/Gojira - Fortitude - 01 - Born For One Thing.mp3"
Title "AC/DC: Live?" → segment "AC_DC_ Live_"
// Within
Within("/media", "films/A/A.mkv") → "/media/films/A/A.mkv", nil
Within("/media", "../etc/passwd") → erreur
Within("/media", "films/../../etc") → erreur
Within("/media", "/etc/passwd") → erreur
Within("/media", "") → erreur
```

- [ ] **Step 2 :** `go test ./naming/` → FAIL.
- [ ] **Step 3 :** implémenter. `PascalCase` : remplacement des lettres accentuées par une table
  (à â ä é è ê ë î ï ô ö ù û ü ÿ ç ñ, majuscules comprises ; œ → oe, æ → ae), sans dépendance
  `x/text`, puis découpe sur tout ce qui n'est ni lettre ASCII ni chiffre, majuscule à la première
  lettre de chaque mot, le reste inchangé. Commentaire à poser :
  `// shortcut: table des accents latins courants, à compléter si un titre la met en défaut`.
  `SafeName` est appliqué à chaque segment de `MusicPath`.
- [ ] **Step 4 :** `go test ./naming/` → PASS.
- [ ] **Step 5 :** commit `feat: noms PascalCase et chemins de rangement`.

### Tâche 4 : index des dossiers existants

**Files :** Create `backend/naming/index.go` ; Test `backend/naming/index_test.go`

**Interfaces :**
- Produces : `type Index struct` (protégé par un `sync.RWMutex`) ; `func BuildIndex(mediaDir string) (*Index, error)` ;
  `func (ix *Index) Lookup(kind Kind, tmdbID int) (name string, ok bool)` ;
  `func (ix *Index) Add(kind Kind, tmdbID int, name string)`.

`BuildIndex` lit, pour `series/` : `<dossier>/tvshow.nfo` ; pour `films/` et `animes/` :
`<dossier>/movie.nfo` puis `<dossier>/<dossier>.nfo`. Il extrait `<tmdbid>…</tmdbid>` par
`encoding/xml` (balise racine quelconque) et ignore les fichiers illisibles ou sans `tmdbid`.

- [ ] **Step 1 : test** — arborescence temporaire : `series/TheBoys/tvshow.nfo` (`<tvshow><tmdbid>76479</tmdbid></tvshow>`),
  `films/Dunkerque/movie.nfo` (`<movie><tmdbid>374720</tmdbid></movie>`), `films/Vide/movie.nfo`
  sans tmdbid. Attendu : `Lookup(Series, 76479) == "TheBoys"`, `Lookup(Movie, 374720) == "Dunkerque"`,
  `Lookup(Anime, 374720)` absent, et après `Add(Anime, 1, "X")`, `Lookup(Anime, 1) == "X"`.
- [ ] **Step 2 → 4 :** FAIL, implémenter, PASS.
- [ ] **Step 5 :** commit `feat: index des dossiers existants par tmdbid`.

### Tâche 5 : base SQLite

**Files :** Create `backend/store/store.go` ; Test `backend/store/store_test.go`

**Interfaces :**
- Produces :
  ```go
  type State string // "pending", "running", "done", "error"
  type Job struct { ID int64; Kind naming.Kind; Title, Ref string; CreatedAt time.Time }
  type File struct {
      ID, JobID int64; Source, Input, OriginalName string; Size int64
      Dest string; Overwrite bool; State State; Error string; Attempts int
      StartedAt, FinishedAt *time.Time
      Meta string // JSON opaque propre à la source (ex. métadonnées Deezer)
  }
  func Open(path string) (*Store, error)
  func (s *Store) Close() error
  func (s *Store) CreateJob(j Job, files []File) (Job, []File, error)   // une transaction
  func (s *Store) Files(states ...State) ([]File, error)                // tri par ID
  func (s *Store) Jobs(ids []int64) (map[int64]Job, error)
  func (s *Store) SetState(id int64, st State, errMsg string) error      // met StartedAt / FinishedAt
  func (s *Store) IncAttempts(id int64) error
  func (s *Store) Replace(id int64, input string) error                  // nouveau lien, état pending, Attempts 0
  func (s *Store) Delete(id int64) error                                 // supprime le job s'il n'a plus de fichier
  func (s *Store) DestTaken(dest string) (bool, error)                   // un fichier non "error" vise déjà dest
  func (s *Store) ResetRunning() error                                   // running → pending (démarrage)
  ```

Schéma : `jobs(id INTEGER PRIMARY KEY, kind TEXT, title TEXT, ref TEXT, created_at TIMESTAMP)` et
`files(id INTEGER PRIMARY KEY, job_id INTEGER REFERENCES jobs(id), source TEXT, input TEXT,
original_name TEXT, size INTEGER, dest TEXT, overwrite INTEGER, state TEXT, error TEXT, attempts
INTEGER, started_at TIMESTAMP, finished_at TIMESTAMP, meta TEXT)`, plus un index sur `files(state)`.
`PRAGMA user_version = 1` à la création. DSN :
`file:<path>?_pragma=journal_mode(DELETE)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)`,
puis `db.SetMaxOpenConns(1)`.

- [ ] **Step 1 : tests** (base dans `t.TempDir()`)
  - `TestSchemaCreatedOnce` : `Open` deux fois sur le même fichier → pas d'erreur, `user_version` = 1,
    `journal_mode` = `delete`.
  - `TestJobLifecycle` : `CreateJob` avec 2 fichiers → `Files(Pending)` en renvoie 2 ;
    `SetState(id, Running, "")` met `StartedAt` ; `SetState(id, Done, "")` met `FinishedAt` ;
    `Files(Done, Error)` renvoie ce fichier.
  - `TestResetRunning` : un fichier `running` → `pending` après `ResetRunning`.
  - `TestDestTaken` : vrai pour un fichier `pending` sur la même destination, faux une fois en `error`.
  - `TestDeleteLastFileDeletesJob`.
- [ ] **Step 2 → 4 :** FAIL, implémenter, PASS (`go get modernc.org/sqlite`).
- [ ] **Step 5 :** commit `feat: base SQLite des jobs et fichiers`.

### Tâche 6 : TMDB, sources HTTP et AllDebrid

**Files :** Create `backend/tmdb/tmdb.go`, `backend/source/source.go`, `backend/source/http.go`,
`backend/source/alldebrid.go` ; Test `backend/tmdb/tmdb_test.go`, `backend/source/http_test.go`,
`backend/source/alldebrid_test.go`

**Interfaces :**
- Produces :
  ```go
  // tmdb
  type Result struct { ID int; Title string; Year int; Animation bool; Poster string; TV bool }
  type Client struct { BaseURL, Key string; HTTP *http.Client }
  func (c *Client) Search(ctx context.Context, title string, year int, tv bool) ([]Result, error)

  // source
  type Item struct {
      Name string; Size int64; Online bool; Error string // Error : message lisible si !Online
      Open func(ctx context.Context, offset int64) (io.ReadCloser, bool, error) // bool : offset respecté
      Track *naming.TrackInfo // musique uniquement
      Meta string             // JSON opaque stocké dans store.File.Meta
  }
  type Source interface {
      Name() string
      Match(input string) bool
      Resolve(ctx context.Context, input string) ([]Item, error)
      // Reopen recrée Item.Open depuis store.File (Input, Meta), après redémarrage.
      Reopen(ctx context.Context, input, meta string) (Item, error)
  }
  func Pick(sources []Source, input string) (Source, bool) // première source dont Match est vrai
  type HTTP struct { Client *http.Client }
  type AllDebrid struct { BaseURL, Key string; HTTP *http.Client; hosts []string }
  func (a *AllDebrid) LoadHosts(ctx context.Context) error
  func (a *AllDebrid) Account(ctx context.Context) (premium bool, until time.Time, err error)
  ```

TMDB v3 : `GET {BaseURL}/search/tv` ou `/search/movie`, paramètres `api_key`, `query`,
`language=fr-FR`, `first_air_date_year` ou `year` si `year > 0`. `Title` = `name` (TV) ou `title`
(film) ; `Year` tiré de `first_air_date` / `release_date` ; `Animation` vrai si `genre_ids` contient
16 ; `Poster` = `https://image.tmdb.org/t/p/w154` + `poster_path` (vide si absent). Une réponse
non 2xx devient `fmt.Errorf("TMDB : %s", status)`.

HTTP : `Match` vrai pour `http://` et `https://`. `Resolve` fait un `HEAD` (repli `GET` avec
`Range: bytes=0-0` si `HEAD` est refusé) : nom depuis `Content-Disposition`, sinon dernier segment
de l'URL décodé ; taille depuis `Content-Length` (ou `Content-Range`) ; `Online` faux et `Error`
« Lien hors ligne (HTTP 404) » pour un statut ≥ 400. `Open` envoie `Range: bytes=<offset>-` si
`offset > 0` et renvoie `true` seulement sur un 206.

AllDebrid v4 (vérifier les détails sur https://docs.alldebrid.com) : en-tête
`Authorization: Bearer <Key>`, paramètre `agent=downloader`. `LoadHosts` lit `GET /v4/hosts` et
garde tous les domaines des hébergeurs ; `Match` vrai si l'hôte du lien (ou un domaine parent) y
figure. `Resolve` appelle `link/infos` (nom, taille, erreur), puis `Open` appelle `link/unlock` au
moment du téléchargement (les liens débridés expirent) et délègue au téléchargement HTTP. Les
erreurs AllDebrid deviennent « <hébergeur> : <message> », par exemple
« 1fichier : fichier supprimé par l'hébergeur » pour `LINK_DOWN`. `Account` lit `GET /v4/user`
(`isPremium`, `premiumUntil`).

- [ ] **Step 1 : tests avec `httptest.Server`**
  - `TestTMDBSearchMovieFrench` : vérifie `language=fr-FR`, `year=2017`, `api_key` ; réponse
    `{"results":[{"id":374720,"title":"Dunkerque","release_date":"2017-07-19","genre_ids":[28,18]}]}`
    → `Result{ID:374720, Title:"Dunkerque", Year:2017, Animation:false}`.
  - `TestTMDBAnimationGenre` : `genre_ids:[16,10751]` → `Animation == true`.
  - `TestHTTPResolveContentDisposition` : `Content-Disposition: attachment; filename="A.mkv"`,
    `Content-Length: 1234` → `Item{Name:"A.mkv", Size:1234, Online:true}`.
  - `TestHTTPOpenRangeIgnored` : le serveur renvoie 200 à une demande `Range` → `Open(ctx, 10)` renvoie `false`.
  - `TestAllDebridLinkDown` : `link/infos` renvoie l'erreur `LINK_DOWN` pour 1fichier →
    `Online == false`, `Error == "1fichier : fichier supprimé par l'hébergeur"`.
  - `TestAllDebridMatch` : après `LoadHosts` (`1fichier.com`), `Match("https://1fichier.com/?abc")`
    vrai, `Match("https://example.org/a.mkv")` faux ; l'en-tête `Authorization` est présent.
  - `TestPickOrder` : avec `[deezer factice, alldebrid, http]`, un lien 1fichier choisit alldebrid,
    un lien quelconque choisit http.
- [ ] **Step 2 → 4 :** FAIL, implémenter, PASS.
- [ ] **Step 5 :** commit `feat: client TMDB et sources HTTP / AllDebrid`.

### Tâche 7 : Deezer

**Files :** Create `backend/deezer/crypt.go`, `backend/deezer/client.go`, `backend/deezer/tags.go`,
`backend/source/deezer.go` ; Test `backend/deezer/crypt_test.go`, `backend/deezer/client_test.go`,
`backend/deezer/tags_test.go`, `backend/source/deezer_test.go`

**Interfaces :**
- Produces :
  ```go
  // deezer
  func TrackKey(songID string) []byte
  func NewDecrypter(r io.Reader, key []byte) io.Reader
  type Client struct { APIURL, GatewayURL, MediaURL, ARL string; HTTP *http.Client }
  type Artist struct { ID int; Name, Picture string }
  type Album struct { ID int; Title, Artist, Cover string; Year, Tracks int }
  type Track struct { ID int; Title, Artist, Album string; AlbumID, Duration int }
  type SearchResult struct { Artists []Artist; Albums []Album; Tracks []Track }
  type TrackMeta struct {
      SongID string; Info naming.TrackInfo; Artists []string; Year int; Genre string
      CoverURL string // modèle avec {size}, remplacé par 800 ou 1200
  }
  func (c *Client) Search(ctx context.Context, q string) (SearchResult, error)
  func (c *Client) AlbumTracks(ctx context.Context, albumID int) ([]TrackMeta, error)
  func (c *Client) TrackMeta(ctx context.Context, trackID int) (TrackMeta, error)
  func (c *Client) StreamURL(ctx context.Context, songID string) (string, error) // erreur si pas de MP3_128
  func (c *Client) CheckARL(ctx context.Context) error
  func WriteTags(path string, m TrackMeta, cover []byte) error
  // source
  type Deezer struct { Client *deezer.Client; HTTP *http.Client }
  // Match : liens deezer.com/…/album/<id> ou /track/<id> (avec ou sans préfixe de langue), et les
  // entrées internes "deezer:album:<id>" / "deezer:track:<id>" envoyées par la recherche.
  ```

Déchiffrement (algorithme repris de deemix) : clé = `md5(songID)` en hexadécimal (32 caractères
`h`), puis `key[i] = h[i] ^ h[i+16] ^ "g4el58wc0zvf9na1"[i]` pour `i` de 0 à 15. Le flux est découpé
en blocs de 2048 octets ; les blocs d'indice 0, 3, 6… (indice % 3 == 0) et de taille pleine sont
déchiffrés en Blowfish CBC avec l'IV `00 01 02 03 04 05 06 07` (repartant de cet IV à chaque bloc),
les autres sont recopiés tels quels.

Client : API publique `https://api.deezer.com` (`/search?q=`, `/search/artist`, `/search/album`,
`/album/<id>` avec `tracks`, `genres`, `release_date`, `nb_disk` ; `/track/<id>` pour
`track_position`, `disk_number`, `contributors`). Passerelle `https://www.deezer.com/ajax/gw-light.php`
avec le cookie `arl` : `deezer.getUserData` (`api_token=""`) → `checkForm` (jeton des appels suivants)
et `USER.OPTIONS.license_token` ; `USER.USER_ID == 0` signifie « Deezer : arl expiré ».
`song.getListData` (`sng_ids`) → `TRACK_TOKEN`. `POST https://media.deezer.com/v1/get_url` avec
`license_token`, `media:[{type:"FULL",formats:[{cipher:"BF_CBC_STRIPE",format:"MP3_128"}]}]`,
`track_tokens` → première `sources[].url`, sinon « Deezer : titre indisponible en MP3 128 ».
Le jeton de session est mis en cache et renouvelé une fois sur erreur d'authentification.

Source Deezer : `Resolve` renvoie un `Item` par morceau (`Track` rempli, `Meta` = `TrackMeta` en
JSON). `Open` récupère l'URL, déchiffre à la volée et renvoie toujours `false` pour l'offset (un
morceau repart du début). La pose des tags et de `cover.jpg` se fait après le téléchargement, par le
crochet `Finish` de la tâche 8.

- [ ] **Step 1 : tests**
  - `TestTrackKey` : `hex.EncodeToString(TrackKey("3135556")) == "6c6c666b39662c37652575603c643439"`.
  - `TestDecrypterStripes` : un flux de 4 × 2048 + 100 octets dont le test chiffre les blocs 0 et 3
    (le bloc final de 100 octets reste en clair) redevient l'original après `NewDecrypter`, y compris
    quand le lecteur source rend des morceaux de 700 octets.
  - `TestARLExpired` : passerelle factice renvoyant `USER_ID: 0` → erreur « Deezer : arl expiré ».
  - `TestStreamURLUnavailable` : `get_url` sans source → erreur « Deezer : titre indisponible en MP3 128 ».
  - `TestAlbumTracksMultiDisc` : album `nb_disk: 2` → `Info.DiskCount == 2`, disques et pistes repris.
  - `TestWriteTags` : écrit puis relit avec `id3v2` : titre, artiste, album, artiste de l'album,
    piste « 1/… » ou « 1 », disque, année, genre, image `APIC` présente.
  - `TestDeezerSourceAlbumPartialFailure` (point de vigilance 4) : album de 3 titres dont le 2e
    n'a pas de source → `Resolve` renvoie 3 `Item` ; `Open` du 2e échoue avec le message ci-dessus,
    `Open` des deux autres réussit.
  - `TestDeezerMatch` : `https://www.deezer.com/fr/album/302127`, `https://deezer.com/track/3135556`,
    `deezer:album:302127` reconnus ; `https://1fichier.com/?x` non.
- [ ] **Step 2 → 4 :** FAIL, implémenter, PASS (`go get golang.org/x/crypto github.com/bogem/id3v2/v2`).
- [ ] **Step 5 :** commit `feat: source Deezer (recherche, déchiffrement, tags)`.

### Tâche 8 : file d'attente

**Files :** Create `backend/queue/queue.go`, `backend/queue/download.go` ; Test `backend/queue/queue_test.go`

**Interfaces :**
- Consumes : `store.Store`, `source.Source`, `source.Pick`, `naming.Within`, `naming.Index.Add`, `deezer.WriteTags`.
- Produces :
  ```go
  type Progress struct { Bytes, Total int64; Speed float64 } // octets/s, moyenne glissante sur 5 s
  type Manager struct {
      Store *store.Store; Sources []source.Source; MediaDir string
      Limits map[naming.Kind]int          // Music: 5, autres types cumulés: 3
      Delays []time.Duration              // 10s, 30s, 90s
      Sleep func(context.Context, time.Duration) error // remplaçable en test
      Finish func(f store.File, path string) error      // posé par main : tags Deezer, cover.jpg, Index.Add
  }
  func (m *Manager) Run(ctx context.Context)      // ResetRunning, puis boucle de planification
  func (m *Manager) Wake()                        // nouveau travail disponible
  func (m *Manager) Pause(); func (m *Manager) Resume(); func (m *Manager) Paused() bool
  func (m *Manager) Cancel(fileID int64)          // interrompt un transfert (Retirer)
  func (m *Manager) Progress() map[int64]Progress
  ```

Comportement :
- Les vidéos (séries, films, animés) partagent une limite de 3, la musique a la sienne, 5.
- Fichier temporaire : `<MediaDir>/download/<fileID>.part`. Si `Open` renvoie `false` pour un
  offset > 0, le `.part` est tronqué et le transfert repart de zéro.
- Fin : `Finish` (si non nil), puis `os.MkdirAll` du dossier et `os.Rename` vers
  `naming.Within(MediaDir, Dest)`. Si la destination existe et `Overwrite` est faux : erreur
  « Destination déjà présente ». Un échec de `Finish` ou du déplacement laisse le `.part` et passe en
  `error` ; Réessayer refait seulement la fin.
- Erreurs passagères (statut 5xx, `net.Error`, `io.ErrUnexpectedEOF`) : `IncAttempts`, attente
  `Delays[attempts-1]`, nouvel essai. Au-delà de 3 : `error`. Les autres erreurs vont directement en
  `error` avec leur message.
- `Pause` annule les contextes des transferts en cours (le `.part` reste) et bloque les démarrages ;
  les fichiers repassent en `pending`.

- [ ] **Step 1 : tests** (store temporaire, source factice adossée à un `httptest.Server`, `Sleep` qui
  enregistre les délais sans attendre)
  - `TestDownloadAndMove` : un fichier film → `done`, présent à `films/A/A.mkv`, plus de `.part`.
  - `TestLimits` : 5 vidéos et 6 morceaux bloqués par le serveur → au plus 3 vidéos et 5 morceaux `running` en même temps.
  - `TestResumeWithRange` : `.part` de 100 octets existant, serveur qui gère `Range` → requête
    `bytes=100-`, fichier final identique à la source.
  - `TestResumeRangeIgnored` (point de vigilance 3) : même cas, serveur qui renvoie 200 → fichier
    final identique à la source (pas de 100 octets en double).
  - `TestRetryDelays` : deux 503 puis succès → délais enregistrés `[10s, 30s]`, état `done` ; quatre
    503 → `error`.
  - `TestMoveFailureKeepsPart` : `Finish` qui échoue → état `error`, `.part` toujours présent.
  - `TestDestinationExists` : destination occupée, `Overwrite` faux → `error`, l'existant intact ;
    `Overwrite` vrai → remplacé.
  - `TestPauseResume` : `Pause` pendant un transfert → `pending`, `.part` présent ; `Resume` → `done`.
  - `TestRestartResetsRunning` : un fichier laissé `running` en base → repris par `Run`.
- [ ] **Step 2 → 4 :** FAIL, implémenter, PASS avec `go test -race ./queue/`.
- [ ] **Step 5 :** commit `feat: file d'attente avec reprise et nouvelles tentatives`.

### Tâche 9 : API HTTP et câblage

**Files :** Create `backend/api/types.go`, `backend/api/handlers.go` ; Modify `backend/main.go` ;
Test `backend/api/handlers_test.go`

**Interfaces :**
- Consumes : tout ce qui précède.
- Produces (JSON en camelCase, consommé par le front) :
  ```go
  type Status struct {
      AllDebrid struct{ OK, Premium bool; Until string; Error string } `json:"alldebrid"`
      Deezer    struct{ OK bool; Error string }                        `json:"deezer"`
      FreeBytes int64                                                  `json:"freeBytes"`
  }
  type AnalyzeRequest struct { Links []string }
  type AnalyzeRow struct {
      Input, Name string; Size int64; Online bool; Error string
      Kind naming.Kind; TMDB *tmdb.Result; Season, Episode int; NeedsReview bool
      Dest string; Exists bool // Exists : « Déjà présent », ligne décochée par défaut
  }
  type QueueRequest struct {
      Kind naming.Kind; Title string; Ref string // Ref : "tmdb:<id>" ou "deezer:album:<id>"
      Files []struct{ Input, Name string; Size int64; Dest string; Overwrite bool }
      Deezer string // entrée Deezer : lien ou "deezer:album:<id>" / "deezer:track:<id>" ; Files vide alors
  }
  type FileView struct {
      ID int64; Name, Dest string; State store.State; Error string
      Bytes, Total int64; Speed float64; Attempts int
  }
  type JobView struct { ID int64; Kind naming.Kind; Title string; Files []FileView }
  type QueueView struct { Paused bool; Speed float64; Jobs []JobView } // jobs avec au moins un fichier non done
  type HistoryRow struct { FinishedAt time.Time; Title, Name, Dest string; Kind naming.Kind; Size int64; State store.State; Error string }
  ```
  Routes :
  `GET /api/status`, `POST /api/videos/analyze`, `GET /api/tmdb/search?q=&tv=&year=`,
  `GET /api/deezer/search?q=` (chaque album porte `present`), `POST /api/queue`, `GET /api/queue`,
  `GET /api/history?filter=all|video|music|error`, `POST /api/files/{id}/retry`,
  `POST /api/files/{id}/replace` (`{"input": "…"}`), `DELETE /api/files/{id}`,
  `POST /api/pause`, `POST /api/resume`, `GET /version`.

Règles :
- Les routes non `GET` passent par `http.NewCrossOriginProtection()`.
- Analyse : pour chaque lien, `source.Pick` → `Resolve`, `naming.ParseFilename`, `tmdb.Search`
  (série si `HasEpisode` ou `Season > 0`), type par la règle de la spec, nom par `Index.Lookup` puis
  `PascalCase`. Les liens sont résolus en parallèle (8 au plus), et un titre n'est cherché qu'une
  fois sur TMDB par analyse. Sans fiche TMDB : `PascalCase(parsed.Title)`, `NeedsReview` vrai,
  `Error` « TMDB : aucune fiche ». Série sans épisode : `NeedsReview` vrai, `Dest` vide.
- `POST /api/queue` : chaque `Dest` passe par `naming.Within`, sinon 400 « Destination invalide :
  <dest> » et rien n'est créé. Une destination déjà visée (`DestTaken`) ou un lien déjà présent dans
  un fichier non terminé → 409 « Déjà dans la file : <dest> ». Après création, `Manager.Wake()`.
- `status` est mis en cache 10 minutes ; `freeBytes` via `syscall.Statfs` sur `MediaDir`.
- Aucun champ de configuration secret n'apparaît dans une réponse.

Câblage `main.go` : `store.Open(DataDir/downloader.db)`, `naming.BuildIndex`, `tmdb.Client`
(`https://api.themoviedb.org/3`), `AllDebrid` (`https://api.alldebrid.com` ; `LoadHosts` au
démarrage, erreur journalisée sans arrêt), `deezer.Client`, sources dans l'ordre
`[deezer, alldebrid, http]`, `queue.Manager` avec `Limits`, `Delays` et un `Finish` qui, pour la
musique, télécharge la pochette (800 px pour les tags, 1200 px pour `cover.jpg` si absent) et
appelle `deezer.WriteTags`, et pour la vidéo appelle `Index.Add`.

- [ ] **Step 1 : tests** (`httptest` + fausses sources, store temporaire)
  - `TestAnalyzeSeries` : deux liens `The.Boys.S02E01…` / `E02` → 2 lignes `series`,
    `Dest == "series/TheBoys/S02/TheBoys.S02.E01.mkv"` ; TMDB appelé une seule fois.
  - `TestAnalyzeAnime` : fiche film avec `Animation` → `Kind == "anime"`, `Dest` sous `animes/`.
  - `TestAnalyzeReusesFolder` : index contenant `76479 → "the boys"` → `Dest` sous `series/the boys/`.
  - `TestAnalyzeNoTMDB` (point de vigilance 5) : TMDB sans résultat → `NeedsReview`, `Error`
    « TMDB : aucune fiche », `Dest == "films/MonFilm/MonFilm.mkv"` pour `Mon.Film.2020.mkv`.
  - `TestAnalyzeExists` : destination déjà présente sur disque → `Exists == true`.
  - `TestQueueRejectsEscape` (point de vigilance 2) : `Dest` `../x.mkv`, `/etc/x` ou
    `films/../../x` → 400, aucune ligne en base.
  - `TestQueueDuplicate` (point de vigilance 1) : le même `QueueRequest` deux fois → 201 puis 409,
    un seul fichier en base.
  - `TestCrossOriginRejected` : `POST /api/queue` avec `Sec-Fetch-Site: cross-site` → 403.
  - `TestStatusHidesSecrets` : le corps de `/api/status` ne contient ni la clé AllDebrid ni l'arl.
  - `TestQueueViewGroupsByJob` : 1 job de 3 fichiers (1 done, 1 running, 1 pending) → 1 `JobView`
    de 3 `FileView`.
- [ ] **Step 2 → 4 :** FAIL, implémenter, PASS ; `go vet ./... && golangci-lint run`.
- [ ] **Step 5 :** commit `feat: API HTTP et câblage du serveur`.

### Tâche 10 : socle front, en-tête et onglets

**Files :** Create `frontend/` (`ng new downloader --style=css --ssr=false --routing`, composants
standalone), `frontend/src/app/api/models.ts`, `frontend/src/app/api/api.service.ts`,
`frontend/src/app/app.ts`, `frontend/src/app/app.routes.ts`, `frontend/src/styles.css`,
`frontend/src/app/format.ts` ; Test `frontend/src/app/format.spec.ts`

**Interfaces :**
- Consumes : routes et types de la tâche 9.
- Produces : `models.ts` reprend les types de `api/types.go` en TypeScript ; `ApiService` expose
  `status()`, `queue()` (relu toutes les 2 s : `httpResource` + `setInterval` qui appelle
  `reload()`), `history(filter: Signal<string>)`, et des méthodes `Promise` pour les écritures
  (`analyze`, `enqueue`, `retry`, `replace`, `remove`, `pause`, `resume`, `tmdbSearch`,
  `deezerSearch`). `format.ts` : `formatBytes(n)` (« 4,8 Go », « 950 Mo », virgule française),
  `formatSpeed(bps)` (« 86 Mo/s »), `formatEta(bytesLeft, bps)` (« 26 s », « 3 min », « 1 h 05 »).
  Routes : `''` → `running`, `videos`, `music`, `history`.

Le shell reprend la maquette : en-tête avec marque, onglets, pastilles AllDebrid (premium et date),
Deezer, espace libre ; sous 700 px de large, onglets dans une barre fixe en bas de l'écran, compteur
d'éléments actifs ou en erreur sur « En cours ». `angular.json` : `outputPath` qui produit
`dist/downloader/browser`, recopié par `task build:front` dans `backend/web/browser/`. Ajouter le
job Front à `.github/workflows/test.yml` (voir tâche 1).

- [ ] **Step 1 :** tests de `format.ts` : `formatBytes(4_800_000_000) == "4,8 Go"`,
  `formatBytes(950_000_000) == "950 Mo"`, `formatSpeed(86_000_000) == "86 Mo/s"`,
  `formatEta(2_000_000_000, 86_000_000) == "23 s"`, `formatEta(3_900_000_000_000, 1_000_000_000) == "1 h 05"`.
- [ ] **Step 2 → 4 :** FAIL, implémenter, PASS (`npx ng test --watch=false`, puis avec `TZ=UTC`).
- [ ] **Step 5 :** vérifier le shell en mobile (390 × 844) et ordinateur (1280 × 800) contre la
  maquette, dans un navigateur, avec le backend lancé localement.
- [ ] **Step 6 :** commit `feat: socle front, en-tête et onglets`.

### Tâche 11 : onglet En cours

**Files :** Create `frontend/src/app/running/running.ts`, `running.html`, `running.css`,
`frontend/src/app/running/group.ts` ; Test `frontend/src/app/running/group.spec.ts`

**Interfaces :**
- Consumes : `ApiService.queue()`, `retry`, `replace`, `remove`, `pause`, `resume`.
- Produces : `function summarize(view: QueueView): { active, pending, errors, bytesLeft, speed }` et
  `function episodeCells(job: JobView): { label: string; state: 'done' | 'running' | 'pending' | 'error'; percent?: number }[]`
  (libellé `E01`… tiré de `Dest`).

Écran (maquette « B · Bureau », onglet En cours) : bandeau de pastilles (débit, actifs et en attente,
restant et durée, erreurs), filtre Tout / Séries / Films / Animés / Musique, bouton « Tout mettre en
pause » / « Reprendre » ; carte Erreurs (Réessayer, Remplacer le lien avec un champ en ligne,
Retirer) ; carte En cours, une ligne par job avec barre globale, cases d'épisodes pour une série,
« 7 / 12 titres » pour un album, chemin de destination commun ; carte « Rangés aujourd'hui ».

- [ ] **Step 1 :** tests de `summarize` (job de 3 fichiers dont 1 erreur → `errors == 1`) et de
  `episodeCells` (fichiers `…S02.E01.mkv` done, `…E03.mkv` running à 62 %, → cellules dans l'ordre).
- [ ] **Step 2 → 4 :** FAIL, implémenter, PASS.
- [ ] **Step 5 :** vérification visuelle mobile et ordinateur avec une file réelle (quelques liens HTTP de test).
- [ ] **Step 6 :** commit `feat: onglet En cours`.

### Tâche 12 : onglet Vidéos

**Files :** Create `frontend/src/app/videos/videos.ts`, `videos.html`, `videos.css`,
`frontend/src/app/videos/rows.ts` ; Test `frontend/src/app/videos/rows.spec.ts`

**Interfaces :**
- Consumes : `ApiService.analyze`, `tmdbSearch`, `enqueue`.
- Produces : `function toRequests(rows: EditableRow[]): QueueRequest[]` — regroupe les lignes cochées
  et en ligne par fiche TMDB (ou par nom quand il n'y a pas de fiche), un `QueueRequest` par groupe.
  `EditableRow` = `AnalyzeRow` plus `checked: boolean` (faux si `!online`, `exists` ou `needsReview`
  sans destination) et la destination modifiable.

Écran : zone de collage, bouton « Analyser », tableau (nom reçu, taille, pastille en ligne, fiche
TMDB avec « changer » qui ouvre une liste de résultats `tmdbSearch`, type modifiable qui recalcule la
destination côté front avec les mêmes règles que `naming`, destination modifiable au clic, « Déjà
présent »), lignes d'une série au-delà de deux repliées, ligne de bonus avec choix « numéro
d'épisode » ou « Bonus », bouton « Lancer N téléchargements ». Un 409 ou un 400 affiche le message du
serveur sur la ligne. Sur mobile : une carte par œuvre, bouton collé en bas. Après lancement, retour
sur « En cours ».

- [ ] **Step 1 :** tests de `toRequests` : 8 épisodes de The Boys + Dunkerque → 2 requêtes ; une
  ligne hors ligne et une ligne `exists` non cochée sont exclues ; une ligne `exists` cochée part avec
  `overwrite: true`.
- [ ] **Step 2 → 4 :** FAIL, implémenter, PASS.
- [ ] **Step 5 :** vérification visuelle mobile et ordinateur.
- [ ] **Step 6 :** commit `feat: onglet Vidéos`.

### Tâche 13 : onglets Musique et Historique

**Files :** Create `frontend/src/app/music/music.ts`, `music.html`, `music.css`,
`frontend/src/app/history/history.ts`, `history.html`, `history.css`

**Interfaces :**
- Consumes : `ApiService.deezerSearch`, `enqueue` (avec `deezer`), `history(filter)`.

Musique : champ de recherche (déclenchée 300 ms après la dernière frappe ; un lien Deezer collé est
proposé directement en « Télécharger »), filtre Tout / Artistes / Albums / Titres, artiste, grille
d'albums (« Télécharger » ou « Déjà présent » grisé), liste de titres avec « + ». Un clic envoie
`{deezer: "deezer:album:<id>"}` ou `{deezer: "deezer:track:<id>"}` et affiche « Ajouté ».
Historique : tableau (terminé, élément, type, taille, rangé dans), filtre Tout / Vidéos / Musique /
Erreurs ; sur mobile, une carte par ligne.

- [ ] **Step 1 :** vérification visuelle des deux onglets, mobile et ordinateur, avec le vrai compte Deezer.
- [ ] **Step 2 :** `npx ng lint && npx ng build && npx ng test --watch=false` → PASS.
- [ ] **Step 3 :** commit `feat: onglets Musique et Historique`.

### Tâche 14 : image Docker et publication

**Files :** Create `Dockerfile`, `.github/workflows/publish.yml`

`Dockerfile` en trois étapes :
1. `node:24` : `npm ci` sur `package.json` et `package-lock.json` copiés seuls (cache), puis copie de
   `frontend/` et `npx ng build`.
2. `golang:1.26` : `go mod download` sur `go.mod` et `go.sum` copiés seuls, copie de `backend/` et de
   `dist/downloader/browser/` dans `web/browser/`, puis, après les `ARG VERSION=dev`, `COMMIT=none`,
   `DATE=unknown` (pour ne pas invalider le cache des modules),
   `CGO_ENABLED=0 go build -ldflags="-X 'github.com/mgarnier-home/utils/buildinfo.Version=${VERSION}' -X '…buildinfo.Commit=${COMMIT}' -X '…buildinfo.Date=${DATE}'" -o /downloader .`
3. `gcr.io/distroless/static` : `/downloader`, `EXPOSE 8080`, `ENTRYPOINT ["/downloader"]`.

`publish.yml` : sur push d'un tag `v*.*.*`, `concurrency` sans annulation, `permissions: contents: read`.
Job « version » : checkout complet (`fetch-depth: 0`), tag lu par variable d'environnement (jamais
interpolé dans le script), contrôle `^v[0-9]+\.[0-9]+\.[0-9]+$` et
`git merge-base --is-ancestor "$GITHUB_SHA" origin/main`, sorties `version` (sans le `v`) et `date`
(UTC ISO 8601). Job « image », sur un runner `linux/amd64` auto-hébergé (déjà authentifié auprès de
Docker Hub) : `docker buildx build` avec les trois `--build-arg`, tags
`mgarnier11/downloader:<version>` et `mgarnier11/downloader:latest`, `--push`. amd64 seulement.

- [ ] **Step 1 :** `docker build -t downloader:test .` → succès.
- [ ] **Step 2 :** lancer l'image avec un `.env` rempli par le propriétaire, un dossier médias temporaire monté sur
  `/media` et un dossier sur `/data` ; ajouter un lien HTTP de test et un titre Deezer ; vérifier
  qu'ils arrivent à leur destination, que `GET /version` répond, et qu'un redémarrage du conteneur
  conserve la file.
- [ ] **Step 3 :** commit `ci: image Docker et publication`.
- [ ] **Step 4 :** Définition de terminé complète, push de `release/v0.1.0`, PR
  `v0.1.0 : première version` vers `main`, suivi de `gh pr checks --watch`.

Hors de ce plan : les variables d'environnement et le déploiement, gérés par le propriétaire.
