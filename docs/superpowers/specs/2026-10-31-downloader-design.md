# Downloader — conception (v0.1.0)

Validée par le propriétaire le 31/10/2026.

## Objectif

Une application web personnelle qui télécharge des vidéos et de la musique et les **range toute
seule** dans la médiathèque, au format attendu par Jellyfin (vidéos) et Navidrome (musique). Elle
remplace deux outils : un téléchargeur de liens d'hébergeurs, dont les dossiers sont saisis à la
main, et un téléchargeur Deezer séparé.

Critères de réussite :

- Coller une série de liens 1fichier d'une saison place chaque épisode dans
  `series/<Nom>/Sxx/<Nom>.Sxx.Eyy.<ext>`, sans rien taper d'autre que la validation du tableau.
- Un film reçu avec son titre anglais est rangé sous son titre français (`Dunkirk` → `Dunkerque`).
- Un album Deezer trouvé par la recherche arrive dans `musics/<Artiste>/<Album>/` avec ses tags et
  sa pochette.
- Le tout fonctionne aussi sur mobile.

Hors périmètre de la v0.1.0 : torrents et magnets (prévus plus tard, voir « Sources »), comptes
multiples, notifications, rafraîchissement de Jellyfin ou Navidrome (ils scannent seuls), playlists
Deezer, discographie complète d'un artiste, fichiers `.nfo` et images (produits par Jellyfin et
Navidrome).

## Interface

Maquettes : `docs/maquettes/` (variante B). Thème sombre unique, IBM Plex Sans et IBM Plex Mono.

En-tête : pastilles d'état AllDebrid (premium, date d'expiration), Deezer (`arl` valide) et espace
libre sur la médiathèque. Quatre onglets ; sur mobile, ils passent dans une barre en bas de l'écran.

### En cours (onglet par défaut)

- Bandeau : débit total, nombre d'actifs et d'éléments en attente, volume restant, nombre d'erreurs,
  filtre par type (Tout, Séries, Films, Animés, Musique), bouton « Tout mettre en pause ».
- Carte **Erreurs** en tête, avec les actions Réessayer, Remplacer le lien, Retirer.
- Carte **En cours**, regroupée par œuvre ou par album : barre de progression globale, chemin de
  destination. Une saison affiche une case par épisode (rangé, pourcentage, en attente). Un album
  affiche « 7 / 12 titres ».
- Carte **Rangés aujourd'hui**, avec un lien vers l'historique.
- Le compteur de l'onglet indique les éléments actifs ou en erreur.

### Vidéos

1. Une zone de texte où l'on colle des liens (un par ligne).
2. Un tableau de vérification, une ligne par fichier : nom reçu, taille, lien en ligne ou non,
   fiche TMDB (avec « changer »), type, destination (modifiable au clic). Les épisodes d'une même
   série au-delà des deux premiers sont repliés en une ligne. Un lien hors ligne s'affiche en rouge
   et n'est pas ajouté. Une destination déjà occupée est marquée « Déjà présent » et décochée.
3. Le bouton « Lancer N téléchargements ».

Sur mobile, le tableau devient une carte par œuvre, et le bouton Lancer reste collé en bas.

### Musique

Un champ qui accepte une recherche Deezer ou un lien Deezer (album ou titre), avec un filtre Tout,
Artistes, Albums, Titres. Les résultats affichent l'artiste, une grille d'albums (bouton
« Télécharger », ou « Déjà présent » grisé) et une liste de titres (bouton « + »). Un clic met en
file, sans étape de vérification.

### Historique

Un tableau des éléments terminés ou en erreur (date, élément, type, taille, dossier), filtrable par
Tout, Vidéos, Musique, Erreurs.

## Architecture

Un seul binaire Go sert l'API sous `/api/` et le front Angular compilé, embarqué avec `embed`.
L'image Docker est construite en plusieurs étapes (build Node, puis build Go).

```
backend/
  main.go, config.go       câblage, routes, front embarqué ; variables d'environnement
  api/                     handlers HTTP, types.go (structs JSON)
  source/                  interface Source + une implémentation par fichier
    alldebrid.go, http.go, deezer.go
  deezer/                  client de l'API Deezer, déchiffrement, tags ID3v2
  tmdb/                    client TMDB
  naming/                  lecture des noms de fichier, PascalCase, calcul des chemins
  queue/                   file d'attente, téléchargements
  store/                   base SQLite (jobs, files)
frontend/                  Angular, composants standalone, signals, httpResource
```

### Sources

Une source transforme une entrée de l'utilisateur en fichiers à télécharger :

```go
type Source interface {
    // Match indique si la source sait traiter cette entrée (lien ou identifiant).
    Match(input string) bool
    // Resolve renvoie les fichiers à télécharger, avec leurs métadonnées.
    Resolve(ctx context.Context, input string) ([]Item, error)
}
```

`Item` porte au minimum : le nom de fichier d'origine, la taille si elle est connue, la disponibilité,
une fonction d'ouverture du flux (URL directe, ou flux déchiffré pour Deezer) et, pour la musique,
les métadonnées des tags.

Sources de la v0.1.0 :

- `alldebrid` : liens des hébergeurs supportés par AllDebrid (1fichier, turbobit, darkibox…).
  Vérifie la disponibilité puis débride en lien direct.
- `http` : lien HTTP ou HTTPS direct, pour tout ce qu'aucune autre source ne reconnaît.
- `deezer` : lien d'album ou de titre Deezer, ou identifiant venu de la recherche. Renvoie un
  `Item` par morceau.

Les sources sont essayées dans cet ordre : `deezer`, `alldebrid` (d'après la liste des hébergeurs
renvoyée par AllDebrid, relue au démarrage), puis `http`. Les torrents et magnets s'ajouteront comme
une source de plus, sans toucher à la file d'attente.

### Flux vidéo

1. `POST /api/videos/analyze` avec les liens : chaque lien passe à la première source qui le
   reconnaît, et donne un nom de fichier, une taille et sa disponibilité.
2. `naming` extrait du nom le titre, l'année et le `SxxEyy`.
3. `tmdb` cherche la fiche (TV si `SxxEyy`, film sinon) en `fr-FR` et propose le premier résultat.
   `GET /api/tmdb/search` sert au bouton « changer ».
4. `naming` calcule la destination (voir « Règles de rangement »).
5. Le front affiche le tableau ; l'utilisateur corrige si besoin, puis `POST /api/queue` ajoute les
   lignes cochées.

### Flux musique

1. `GET /api/deezer/search?q=` renvoie artistes, albums et titres, chaque album portant un indicateur
   « déjà présent ».
2. `POST /api/queue` avec un album, un titre ou un lien Deezer : la source `deezer` renvoie les
   morceaux, la destination vient de leurs tags.

### File d'attente

- Un seul gestionnaire pour tous les types : 3 vidéos et 5 morceaux en parallèle au maximum.
- États d'un fichier : `en attente`, `en cours`, `rangé`, `erreur`. Une pause globale suspend les
  démarrages et interrompt les transferts en cours, qui reprendront au même endroit.
- Chaque fichier est écrit dans `<médiathèque>/download/`, puis déplacé à sa destination une fois
  terminé. Le dossier temporaire est sur le même système de fichiers que la destination, donc le
  déplacement est un `rename` instantané : Jellyfin et Navidrome ne voient jamais de fichier
  partiel.
- Les dossiers de destination sont créés au besoin.
- Une vidéo interrompue reprend avec un en-tête HTTP `Range`, à partir de la taille du fichier
  partiel déjà présent dans `download/`. Au démarrage, les fichiers `en cours` repartent d'où ils en
  étaient (ou du début si le serveur ne gère pas `Range`).
- `GET /api/queue` renvoie l'état complet ; le front le relit toutes les 2 secondes.

### Persistance

La file et l'historique sont enregistrés dans une base SQLite, `<DOWNLOADER_DATA_DIR>/downloader.db`,
avec le pilote `modernc.org/sqlite` (Go pur, le binaire reste construit sans CGO). La base est
ouverte avec `journal_mode=DELETE` (pas de WAL, qui repose sur une mémoire partagée inutilisable sur
NFS), `busy_timeout=5000` et une seule connexion (`SetMaxOpenConns(1)`).

- Table `jobs` : un ajout de l'utilisateur (une œuvre, un album ou un titre). Colonnes : identifiant,
  type (série, film, animé, musique), titre affiché, fiche TMDB ou Deezer, date de création.
- Table `files` : un fichier à télécharger, rattaché à un job. Colonnes : identifiant, job, source,
  entrée d'origine (lien ou identifiant Deezer), nom d'origine, taille, destination, état, message
  d'erreur, nombre de tentatives, dates de début et de fin.
- Le schéma est créé au démarrage s'il n'existe pas (`CREATE TABLE IF NOT EXISTS`). Une version de
  schéma (`PRAGMA user_version`) permettra les migrations des versions suivantes.
- Chaque changement d'état est écrit dans une transaction. La progression en octets et le débit ne
  sont gardés qu'en mémoire : à la reprise, la taille du fichier partiel suffit.
- L'historique est la liste des fichiers `rangé` ou `erreur`, sans purge dans la v0.1.0.

La base vit sur le partage NFSv4 des données du homelab, comme les autres données de conteneurs.
C'est acceptable parce qu'un seul processus, sur un seul hôte, y accède, et que NFSv4 gère les
verrous `fcntl` utilisés par le journal classique. Deux instances ne doivent jamais ouvrir la même
base. Si la base pose problème, le repli prévu est un disque local répliqué par Litestream, sans
changement de code.

## Règles de rangement

Racine de la médiathèque : `/media` dans le conteneur. Dossiers : `series/`, `films/`, `animes/`,
`musics/`, `download/` (temporaire).

### Lecture du nom de fichier

On retire l'extension et les balises de release (langue, résolution, source, codecs, audio, HDR,
groupe après le dernier `-`), puis on extrait :

- `SxxEyy` (aussi `S01.E01` et `1x01`) → saison et épisode ;
- une année entre 1900 et l'année courante + 1 → année ;
- ce qui précède le premier de ces éléments → titre, les points et `_` remplacés par des espaces.

La table de tests reprend de vrais noms reçus, par exemple
`The.Boys.S01E04.MULTi.HDR.2160p.WEB.DTS-HD.MA.5.1.H265-Wawacity.red.mkv` →
titre `The Boys`, S01, E04.

### Type proposé

- un fichier avec `SxxEyy` → **Série** ;
- une fiche de film TMDB avec le genre Animation (id 16) → **Animé** ;
- sinon → **Film**.

Une série d'animation reste une série. Le type est modifiable dans le tableau.

### Nom

1. On cherche d'abord un dossier existant : un `.nfo` (`tvshow.nfo`, `movie.nfo` ou `<Nom>.nfo`)
   de `series/`, `films/` ou `animes/` dont le `<tmdbid>` est celui de la fiche. Si on le trouve, on
   reprend le nom de ce dossier tel quel. L'index est construit au démarrage puis complété à chaque
   rangement.
2. Sinon, on passe le titre français TMDB en PascalCase : suppression des accents, chaque suite de
   lettres ou de chiffres devient un mot, première lettre de chaque mot en majuscule, mots collés.
   *Spirit : l'indomptable* → `SpiritLIndomptable` ; *Dune : Première partie* →
   `DunePremierePartie`.
3. Le nom reste modifiable dans le tableau. Les numéros de suite que TMDB ne donne pas
   (`Avatar` → `Avatar1`) se corrigent à la main.

### Chemins vidéo

| Type  | Chemin                                         |
| ----- | ---------------------------------------------- |
| Série | `series/<Nom>/S<ss>/<Nom>.S<ss>.E<ee>.<ext>`  |
| Film  | `films/<Nom>/<Nom>.<ext>`                      |
| Animé | `animes/<Nom>/<Nom>.<ext>`                     |

Saison et épisode sur deux chiffres. L'extension d'origine est gardée. Un fichier de série sans
épisode détecté passe en « à vérifier » : l'utilisateur choisit un numéro d'épisode, ou `Bonus`, qui
le range dans `series/<Nom>/S<ss>/Bonus/` sous son nom d'origine nettoyé.

### Chemins musique

`musics/<Artiste de l'album>/<Album>/<Artiste> - <Album> - <NN> - <Titre>.mp3`

- `NN` : numéro de piste sur deux chiffres.
- Album à plusieurs disques : `musics/<Artiste>/<Album>/CD<n>/…`.
- Un titre seul est rangé dans le dossier de son album, sous le même nom.
- Les caractères interdits dans un nom de fichier (`/ \ : * ? " < > |`) sont remplacés par `_`.
- Qualité : MP3 128 kbps.
- Tags ID3v2 : titre, artistes, album, artiste de l'album, piste, disque, année, genre, pochette
  intégrée en 800 px.
- `cover.jpg` en 1200 px dans le dossier de l'album.
- Un album est « déjà présent » si son dossier existe.

### Destination occupée

Une ligne dont la destination existe déjà est marquée « Déjà présent » et décochée. Cochée, elle
remplace le fichier existant au moment du déplacement.

## Erreurs

- Message lisible et action proposée pour chaque erreur : « 1fichier : fichier supprimé par
  l'hébergeur » (Remplacer le lien, Retirer), « Deezer : arl expiré » (Réessayer après mise à jour du
  secret), « TMDB : aucune fiche » (saisie manuelle du nom).
- Les erreurs passagères (réponse 5xx, coupure réseau) sont retentées 3 fois, après 10 s, 30 s puis
  90 s. Au-delà, le fichier passe en `erreur`.
- Un déplacement qui échoue laisse le fichier dans `download/` et passe en `erreur` : rien n'est
  perdu, Réessayer relance seulement le déplacement.
- Les pastilles de l'en-tête sont rafraîchies toutes les 10 minutes.

## Sécurité

- Toute destination, calculée ou saisie à la main, est nettoyée (`filepath.Clean`), puis refusée si
  elle sort de la racine de la médiathèque. Cela couvre aussi les tags Deezer qui contiendraient
  `..` ou `/`.
- Les secrets ne sont jamais renvoyés par l'API ni écrits dans les logs.
- Aucune authentification dans l'application : elle reste derrière le reverse proxy du homelab,
  comme l'outil qu'elle remplace.

## Configuration

| Variable                       | Rôle                                  | Défaut     |
| ------------------------------ | ------------------------------------- | ---------- |
| `DOWNLOADER_SERVER_PORT`       | port d'écoute                         | `8080`     |
| `DOWNLOADER_MEDIA_DIR`         | racine de la médiathèque              | `/media`   |
| `DOWNLOADER_DATA_DIR`          | dossier de `downloader.db`            | `/data`    |
| `DOWNLOADER_ALLDEBRID_API_KEY` | clé API AllDebrid                     | (requise)  |
| `DOWNLOADER_DEEZER_ARL`        | cookie `arl` du compte Deezer         | (requise)  |
| `DOWNLOADER_TMDB_API_KEY`      | clé API TMDB (lecture)                | (requise)  |

Les trois secrets sont fournis par le propriétaire et ne sont jamais versionnés. Le conteneur tourne
avec l'utilisateur propriétaire de la médiathèque, pour que les fichiers rangés gardent les bons
droits.

## Tests

Tests unitaires Go sur la logique non triviale :

- lecture des noms de fichier, sur une table de vrais noms ;
- PascalCase et calcul des chemins (séries, films, animés, musique, multi-disques, bonus) ;
- refus des chemins qui sortent de la médiathèque ;
- déchiffrement Deezer, sur un petit échantillon chiffré de référence ;
- file d'attente : limites de parallélisme, reprise après redémarrage, nouvelles tentatives,
  déplacement final ;
- `store` : création du schéma et transitions d'état, sur une base SQLite temporaire ;
- clients AllDebrid, TMDB et Deezer contre des serveurs `httptest`.

Front : `ng test` sur les composants qui portent de la logique (tableau de vérification, regroupement
de l'onglet En cours).

## Livraison

- Monorepo `backend/` et `frontend/`, `taskfile.yml` pour construire, Dockerfile multi-étapes.
- Workflow `test.yml` sur chaque PR vers `main`, workflow `publish.yml` sur un tag `v*.*.*`, qui
  publie l'image.
- La v0.1.0 est développée sur `release/v0.1.0` et fusionnée par une PR vers `main`.
