# Downloader — packs de séries (v0.2.0)

Validée par le propriétaire le 31/10/2026.

## Objectif

Un pack est une archive `.zip` ou `.rar`, derrière un seul lien, qui contient une ou plusieurs
saisons d'une série. Le downloader la télécharge, l'extrait, puis range chaque épisode comme s'il
avait été collé seul.

Critères de réussite :

- Coller le lien d'un pack affiche dès l'analyse la série reconnue et son dossier
  (`series/<Nom>/`), retrouvé dans la médiathèque s'il existe, grâce au nom de l'archive.
- Après téléchargement, chaque épisode est rangé dans `series/<Nom>/Sxx/<Nom>.Sxx.Eyy.<ext>` sans
  rien saisir ; les fichiers sans numéro d'épisode vont dans un dossier `Bonus`.
- Rien n'est jamais écrasé ; l'historique donne le bilan du pack.

Hypothèses retenues :

- Une archive par lien : les archives découpées (`part1.rar`, `.zip.001`) sont hors périmètre.
- Un pack ne contient qu'une série.
- Seules les vidéos sont extraites ; sous-titres, `.nfo` et autres fichiers sont ignorés.
- L'archive et les fichiers temporaires sont supprimés quand le pack a réussi.

Hors périmètre de la v0.2.0 : archives découpées, archives protégées par mot de passe (le pack
passe en erreur), plusieurs séries par pack, sous-titres, reprise d'une extraction interrompue.

## Reconnaissance d'un pack

Un fichier résolu dont le nom finit par `.zip` ou `.rar` (sans tenir compte de la casse) est un
pack : `naming.IsPack(name)`. C'est la seule règle, utilisée par l'analyse, la file et l'historique.

## Analyse

`POST /api/analyze`, pour une ligne pack :

- Le type est forcé à `series`, même sans `Sxx` dans le nom (`Breaking.Bad.Integrale.rar`).
- Le titre vient de `ParseFilename` sur le nom de l'archive ; la recherche TMDB se fait en mode
  série, puis le nom du dossier suit la règle habituelle : dossier existant trouvé par l'index,
  sinon titre TMDB en PascalCase.
- `dest` vaut `series/<Nom>` ; `pack` vaut `true` ; `season` reprend la saison lue dans le nom de
  l'archive (0 sinon), utilisée seulement pour les bonus.
- `exists` reste faux : la présence se vérifie par épisode, après extraction.

`AnalyzeRow` gagne le champ `pack` (booléen).

## Onglet Vidéos

- La ligne d'un pack porte un badge « Pack » et affiche `series/<Nom>/`.
- Pas de champs saison ni épisode ; le type est figé à Série.
- Changer la fiche TMDB recalcule la destination, comme pour les autres lignes.

## Ajout à la file

`POST /api/queue` crée un job série dont le fichier pack porte `meta = {"pack":{}}`.

Le contrôle « destination déjà dans la file » ne s'applique pas à un pack, dont la destination est
un dossier : deux packs d'une même série peuvent attendre ensemble. Le contrôle « lien déjà dans la
file » s'applique toujours.

## Extraction et rangement

### Paquet `archive`

Il lit les ZIP (`archive/zip`) et les RAR 4 et 5 (`github.com/nwaples/rardecode/v2`, pur Go,
dépendance accordée) derrière une seule fonction :

```go
type Entry struct {
	Path string // chemin dans l'archive, séparateur "/"
	Size int64  // taille décompressée
}

// Walk appelle fn pour chaque fichier de l'archive, dans l'ordre ; r n'est valable que pendant l'appel.
func Walk(path string, fn func(e Entry, r io.Reader) error) error
```

L'archive est lue en flux, ce qu'exige un RAR solide. Dossiers et liens symboliques sont ignorés.
Une archive chiffrée renvoie `ErrPassword`.

### Branchement dans la file

- `queue.finish` reconnaît un pack par `naming.IsPack(f.OriginalName)` et confie tout le travail à
  `Finish`, sans le contrôle « destination présente » ni le `rename` habituels. Le `.part` n'est
  supprimé qu'en cas de succès.
- `Finish` reçoit en plus un rappel de progression `func(done, total int64)`, ignoré par la musique.
  Pendant l'extraction, la progression du transfert repart de 0 sur la taille totale des vidéos.
- `FileView` gagne le champ `phase` : vide, ou `extraction` pendant l'extraction d'un pack.

### Traitement d'un pack (`finisher.pack`)

Le nom de la série est le second segment de `f.Dest` (`series/<Nom>`).

1. **Inventaire** (premier passage de `Walk`) : on garde les entrées dont l'extension est `.mkv`,
   `.mp4`, `.avi`, `.m4v` ou `.ts`. Aucune vidéo : erreur « Aucune vidéo dans l'archive ». Si
   l'espace libre de la médiathèque est inférieur au total des vidéos : erreur « Espace insuffisant
   pour extraire », avant toute écriture.
2. **Destination** de chaque vidéo, d'après `ParseFilename` sur le nom du fichier (sans ses
   dossiers) :
   - avec un épisode : `naming.VideoPath(Series, <Nom>, saison, épisode, ext)` ;
   - sans épisode (bonus) : la saison vient du nom du fichier, sinon du dossier parent dans
     l'archive (`S02/…`), sinon du nom de l'archive ; le fichier va dans
     `naming.BonusPath(<Nom>, saison, nom d'origine)`, ou dans `series/<Nom>/Bonus/<nom d'origine>`
     sans saison.
3. **Extraction** (second passage de `Walk`) : chaque vidéo est écrite dans
   `download/<id>/<n>.tmp`, `n` étant son rang ; le chemin de l'entrée n'est jamais utilisé comme
   chemin d'écriture, ce qui écarte les entrées du type `../evil.mkv`. Elle est ensuite déplacée à
   sa destination, dossiers créés au besoin. Si la destination existe déjà, ou a déjà servi dans ce
   pack (même épisode en 720p et en 1080p), la vidéo est comptée « déjà présente » et supprimée.
   Rien n'est écrasé.
4. **Fin** : `meta` devient `{"pack":{"placed":N,"present":N,"bonus":N}}` (`placed` compte les
   épisodes, `bonus` les bonus rangés), l'index reçoit `series/<Nom>` pour la fiche TMDB du job, et
   `download/<id>/` est supprimé, que le pack réussisse ou non.

### Erreurs

Le pack passe en erreur, son `.part` reste pour « Réessayer » :

| Cas                         | Message                                |
| --------------------------- | -------------------------------------- |
| Archive chiffrée            | Archive protégée par mot de passe      |
| Archive illisible           | Archive illisible : `<détail>`         |
| Aucune vidéo                | Aucune vidéo dans l'archive            |
| Espace insuffisant          | Espace insuffisant pour extraire       |
| Écriture ou déplacement     | Extraction : `<détail>`                |

Les épisodes déjà déplacés restent en place. Comme un `.part` complet n'est pas retéléchargé,
« Réessayer » relance seulement l'extraction ; les épisodes déjà rangés y sont comptés « déjà
présents ».

`shortcut:` un arrêt pendant l'extraction la relance entière au démarrage, et le bilan compte alors
en « déjà présent » ce qui avait été rangé avant l'arrêt ; une reprise fine viendra si le besoin
apparaît.

## En cours et historique

- **En cours** : la ligne du pack montre le nom de l'archive et sa barre de téléchargement, puis
  « Extraction » avec sa progression, sans débit.
- **Historique** : `HistoryRow` gagne `pack` (`{placed, present, bonus}` ou `null`), lu dans
  `meta`. La ligne montre le nom de l'archive, le bilan (« 62 épisodes rangés · 2 déjà présents ·
  1 bonus ») et le dossier `series/<Nom>/`. Le filtre par type reste « Série ».
- **Rangés du jour** compte un pack comme un élément.

## Tests

- `archive` : ZIP générés dans le test ; RAR 4, RAR 5 et RAR chiffré de quelques Ko dans
  `testdata/` ; ZIP contenant `../evil.mkv`. Liste des entrées, contenu lu, `ErrPassword`.
- `naming` : `IsPack` ; destination d'un épisode, d'un bonus avec saison venant du fichier, du
  dossier, de l'archive, et sans saison.
- `finisher.pack` : pack complet (épisodes, bonus, déjà présent, doublon interne, bilan), erreurs
  (mot de passe, aucune vidéo, espace insuffisant), suppression de `download/<id>/`, entrée
  `../evil.mkv` écrite sous la médiathèque.
- `queue` : un pack réussi supprime son `.part` ; un pack en erreur le garde ; la progression passe
  en phase `extraction`.
- `api` : analyse d'un lien `.rar` (`pack: true`, série forcée, dossier existant réutilisé), deux
  packs de la même série acceptés dans la file, historique avec bilan.
- Front : ligne Pack de l'onglet Vidéos, libellé « Extraction » dans En cours, bilan dans
  l'historique ; `ng test` lancé aussi avec `TZ=UTC`. Vérification visuelle en mobile et en
  ordinateur.
