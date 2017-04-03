# PocketNPM

PocketNPM is a simple but powerful utility for mirroring the full of npm packages from another npm registry, without heaving to install some *possibly* heavy dependencies such as CouchDB and Nginx.

You can use the test server at `npm.airfly.io`.

## Usage

```bash
$ go get -u github.com/ssut/pocketnpm
$ pocketnpm init # to create a default config file under the current directory
$ pocketnpm start # mirroring
$ pocketnpm start -onetime # disable continuous mirroring
$ pocketnpm start -s # start server
$ pocketnpm -d start # debug mode
$ pocketnpm start -s -only-server # start only server
```

Note that your first time mirroring may take up to a day or more, and it may fail with an error saying that:

- DNS failure: add domain to hosts file (example: `151.101.0.162 registry.npmjs.org`)
- Not enough disk space: free at least 1TB of disk space

Once a mirror has been setup up, PocketNPM will automatically sync your mirror once every interval.

### Install the systemd service

## Why

It's very hard nowadays to mirror the npm repo because [npm separated attachments](http://blog.npmjs.org/post/83774616862/deprecating-fullfatdb) from the main couchdb database(called skim), so now the only way to make a full mirror is either to clone `replicate.npmjs.com` and use [npm-fullfat-registry](https://github.com/npm/npm-fullfat-registry), or to use a lazy mirroring tool such as `local-npm` and `npm-lazy-mirror`.

### Use Cases and Goals

- local npm mirror for offline dev
- decentralized; distributed; anyone should be able to run a *continuous* npm mirror 
- for services like `npmjs.cf` that provides only documents, not attachments, PocketNPM can serve attachments as well.

## How

First, I've tried to create a full clone of `registry.npmjs.org` but I could not have easily done by some reasons:

- [Fullfatdb is no longer available](http://blog.npmjs.org/post/93158668615/reminder-fullfat-db-is-going-away)
- cloning `replicate.npmjs.com` takes a long time -- using 1gbps internet connection it takes roughly 2 hours to clone.
  - The replication process of CouchDB is designed for cases where it's important to have all changes in history, not for npm replication.
  - npm(replicate db) does not preserve past revisions of each document, you can check it simply:
    - Get a revision hash from `https://replicate.npmjs.com/registry/react?revs=true`
    - Check it: `https://replicate.npmjs.com/registry/reactrev=145-7483286c06a6ad00e9560b7e8f70b695`
- `npm-fullfat-registry` is terribly slow and unstable.

Invention:

- Database

  I chose boltdb, an embedded key/value database for Go, instead of requiring third-party database servers.

  **Buckets (collections)**
  - Globals: contains global variables such as "sequence" (couchdb)
  - Packages: contains key value pair of package to check consistency. (key: name, value: revision)
  - Marks: contains the state to determine whether package currently downloaded or not.
  - Documents: contains full document of the package
  - Files: list of files in url object

- Repository Access

  Used CouchDB API.
  
  - Full Docs: `https://replicate.npmjs.com/_all_docs?update_seq=true`
  - Document: `https://replicate.npmjs.com/registry/name`
  - Changes: `https://replicate.npmjs.com/_changes?since=sequence`

- Workflow

  1. If existing sequence is zero, fetch all docs and create a skeleton scheme on the following buckets: `Packages`, `Marks`
  2. If existing sequence is greater than zero but there remains any incomplete packages, enter the `continue` mode, which downloads all packages.
  3. fetch document from the source -> download all package files -> save to `Documents` and `Files` -> mark it as completed (`Marks`)
    - If there is any inconsistency between the prefetched docs and the source, existing revision will be updated.
  4. If existing sequence is greater than zero and all packages are marked as completed, then enter the `sync` mode, which applies changes taken from the source.
  5. fetch all changes -> compare revisions -> update sequence -> mark all different packages as incomplete -> do the step 3. 

### Documentation

None so far. 

## Caveats

A few current caveats with PocketNPM.

### Modern filesystem needed

Packages will be stored in the parent directory by the first character of its name, then the parent directories will contain a lot of subdirectories. As of Mar 2017 when I make a full clone, npm had over 450,000 packages, this means even if all these packages are distributed, you have to take care of the filesystem. In case of ext3, there is a limit of 31998 sub-directories per one directory, stemming from its limit of 32000 links per inode.

### Case-sensitive filesystem needed

You need to run pocketnpm on a case-sensitive filesystem.

macOS natively does this OK even though the filesystem is not strictly case-sensitive and pocketnpm will work fine when running on macOS. However, tarring a pocketnpm data directory and moving it to, e.g. Linux with a case-sensitive filesystem will lead to inconsistencies.

## SSDs are highly recommended for database storage

SSDs are highly recommended for database storage, just like any other database. Using SSDs for data storage is good as well but the performance depends heavily on the database storage.

### You can't clone the mirror itself

It is not a goal of PocketNPM to pretend the real implementation. Therefore you can't use the same features as the public registry, these features may not be implemented.

## License

PocketNPM is an open source project licensed under the MIT license.
