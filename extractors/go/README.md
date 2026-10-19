# Extracteur Go natif

Extracteur natif codetospec pour l'écosystème Go, sur le protocole
`codetospec/facts/v1`. Il émet ce que tree-sitter ne peut pas voir : les
**routes** HTTP, les **constantes** numériques et les **tables** SQL.

## Ce qu'il extrait

- **Routes** (`route`) via `golang.org/x/tools/go/packages` (types résolus) :
  gin/echo (`r.GET("/x", h)`), chi (`r.Get(...)`), net/http
  (`mux.HandleFunc("GET /x", h)`), plus `Handle(method, path, …)` et
  `Any`. Les préfixes de groupe (`api := r.Group("/api")`) sont résolus, et
  le handler est nommé via l'information de type (`pkg.Func`,
  `Type.Méthode`). Son code est localisé (`handler_path`, `handler_lines` :
  la déclaration de la fonction) à travers les formes d'enregistrement
  courantes : fonction ou méthode, closure qui appelle une seule fonction
  (`func(w, r) { handleX(w, r) }`), conversion `http.HandlerFunc(h)`,
  enveloppe qui reçoit le handler (`withAuth(h)`) ; une autre closure est
  elle-même le handler. codetospec relie alors à l'endpoint toute règle qui
  cite ce code. Un handler hors du module (`playground.Handler(…)`) a pour
  code l'enregistrement lui-même, le code le plus proche qui sert la route.
  `-skip-dirs testutil` (liste séparée par des virgules, vide par défaut)
  écarte les routes enregistrées sous ces dossiers, à toute profondeur :
  les serveurs factices des aides de test (`testutil/hcaptcha.go`) ne sont
  pas des points d'entrée. `scanwebapp.sh` le passe.
- **Constantes** (`constant`) : chaque constante numérique que le code
  du module utilise, nommée comme le code l'écrit (`h.StatusBadRequest`
  avec un import aliasé, `maxRetries` nu), avec sa valeur exacte
  (`go/constant`), bibliothèque standard comprise. Le grounding retrouve
  ainsi un « 400 » énoncé derrière `http.StatusBadRequest`. Un nom nu lié à
  deux valeurs (deux paquets) est écarté.
- **Symboles précis** (`symbol`, `precise: true`) : chaque fonction,
  méthode et type nommé du module, avec son span exact, son genre et sa
  signature ; mêmes ids que la couche tree-sitter et le convertisseur SCIP
  (ils fusionnent). Rend scip-go inutile pour Go.
- **Appels** (`call`) : quelle fonction ou méthode du module appelle ou
  utilise quelle autre déclaration du module (une paire par fait, chacune
  citée par son span) ; codetospec place une définition utilisée par les
  appelants d'un seul domaine dans ce domaine.
- **Code partagé atteint** (`reach`, avec `-shared nom=dir`, répétable) :
  chaque déclaration du module partagé (`go.mod` de `dir`) que le module
  analysé atteint — ce que son code nomme, puis ce que ces déclarations
  nomment ; un type atteint atteint ses méthodes ; les interfaces ne sont
  pas suivies (borne basse). Citée sous la racine `nom` (`@nom/chemin`),
  pour `codetospec run --inherit nom=… --ref-root nom=…`. Le module doit
  résoudre le code partagé localement (go.work ou `replace`).
- **Tables** (`table`) : `CREATE TABLE` des fichiers `.sql` (schéma sqlc,
  migrations), avec colonnes typées ; les contraintes de table sont ignorées.

## Prérequis

Le module cible doit être chargeable par `go/packages` (toolchain Go
présente, dépendances résolvables). Les erreurs de compilation partielles
sont tolérées : l'extraction continue avec l'information de type disponible.

## Utilisation

```sh
cd extractors/go && go build -o extract .
```

Entrée `codetospec.yaml` du dépôt analysé :

```yaml
extractors:
  - name: go
    cmd: go
    args: [run, ./extractors/go, --root, "{src}"]
    timeout: 300s
```

Option `--sql-glob "sql/schema/*.sql"` pour restreindre les fichiers de
schéma scannés.
