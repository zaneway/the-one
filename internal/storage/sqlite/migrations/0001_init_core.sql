create table if not exists schema_migration (
  version             integer primary key,
  name                text not null,
  applied_at          datetime not null,
  checksum            text
);

create table if not exists local_identity (
  id                  text primary key,
  display_name        text,
  created_at          datetime not null,
  updated_at          datetime not null
);

create table if not exists workspace (
  id                  text primary key,
  name                text not null,
  root_path           text,
  created_at          datetime not null,
  updated_at          datetime not null
);

create table if not exists project (
  id                  text primary key,
  workspace_id        text not null,
  name                text not null,
  created_at          datetime not null,
  updated_at          datetime not null
);

create table if not exists repo (
  id                  text primary key,
  project_id          text,
  workspace_id        text not null,
  root_path           text not null,
  current_commit      text,
  code_index_provider text,
  created_at          datetime not null,
  updated_at          datetime not null
);
