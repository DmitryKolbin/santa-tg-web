PRAGMA foreign_keys = ON;
CREATE TABLE IF NOT EXISTS users (
 id INTEGER PRIMARY KEY,
 name TEXT NOT NULL,
 address TEXT NOT NULL DEFAULT '',
 wishlist TEXT NOT NULL DEFAULT '',
 location TEXT NOT NULL DEFAULT '',
 destinations TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE IF NOT EXISTS games (
 id INTEGER PRIMARY KEY,
 name TEXT NOT NULL,
 creator INTEGER NOT NULL REFERENCES users(id),
 invite TEXT NOT NULL UNIQUE,
 single_cycle INTEGER NOT NULL DEFAULT 1,
 allow_mutual_pairs INTEGER NOT NULL DEFAULT 1,
 started INTEGER NOT NULL DEFAULT 0,
 round INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS members (
 game INTEGER NOT NULL REFERENCES games(id),
 person INTEGER NOT NULL REFERENCES users(id),
 PRIMARY KEY(game, person)
);
CREATE TABLE IF NOT EXISTS assignments (
 thread TEXT NOT NULL UNIQUE,
 game INTEGER NOT NULL REFERENCES games(id),
 round INTEGER NOT NULL,
 sender INTEGER NOT NULL REFERENCES users(id),
 receiver INTEGER NOT NULL REFERENCES users(id),
 PRIMARY KEY(game, round, sender),
 UNIQUE(game, round, receiver),
 CHECK(sender <> receiver)
);
CREATE TABLE IF NOT EXISTS messages (
 thread TEXT REFERENCES assignments(thread), id INTEGER PRIMARY KEY,
 game INTEGER NOT NULL REFERENCES games(id), round INTEGER NOT NULL,
 sender INTEGER NOT NULL REFERENCES users(id), receiver INTEGER REFERENCES users(id),
 text TEXT NOT NULL, file_key TEXT NOT NULL DEFAULT '', file_name TEXT NOT NULL DEFAULT '',
 file_type TEXT NOT NULL DEFAULT '', created TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS messages_game ON messages(game, id);
CREATE INDEX IF NOT EXISTS messages_thread ON messages(thread, id);
CREATE TABLE IF NOT EXISTS deliveries (
 thread TEXT PRIMARY KEY REFERENCES assignments(thread),
 tracking_number TEXT NOT NULL DEFAULT '', received INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS message_reads (
 person INTEGER NOT NULL REFERENCES users(id), game INTEGER NOT NULL REFERENCES games(id),
 thread TEXT NOT NULL, message INTEGER NOT NULL, PRIMARY KEY(person, game, thread)
);
