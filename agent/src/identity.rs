use std::{
    fs::{self, File, OpenOptions},
    io::{self, BufReader, Write},
    path::{Path, PathBuf},
};

use thiserror::Error;
use uuid::Uuid;

#[derive(serde::Serialize, serde::Deserialize)]
pub struct AgentIdentity {
    pub schema_version: u8,
    pub machine_id: Uuid,
    pub agent_id: Uuid,
    pub agent_secret: String,
}

#[derive(Debug, Error)]
pub enum IdentityError {
    #[error("read or write agent identity state: {0}")]
    Io(#[from] io::Error),
    #[error("parse agent identity state: {0}")]
    Json(#[from] serde_json::Error),
    #[error("agent identity state already exists at {path}")]
    AlreadyExists { path: PathBuf },
    #[error("unsupported agent identity schema version {version}")]
    UnsupportedSchemaVersion { version: u8 },
}

pub struct IdentityStore {
    state_directory: PathBuf,
}

impl IdentityStore {
    pub fn new(state_directory: PathBuf) -> Self {
        Self { state_directory }
    }

    pub fn load(&self) -> Result<Option<AgentIdentity>, IdentityError> {
        let path = self.identity_path();
        let file = match File::open(&path) {
            Ok(file) => file,
            Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(None),
            Err(error) => return Err(error.into()),
        };

        let identity: AgentIdentity = serde_json::from_reader(BufReader::new(file))?;
        validate_schema_version(&identity)?;
        Ok(Some(identity))
    }

    pub fn save_new(&self, identity: &AgentIdentity) -> Result<(), IdentityError> {
        validate_schema_version(identity)?;

        let destination = self.identity_path();
        if destination.exists() {
            return Err(IdentityError::AlreadyExists { path: destination });
        }

        fs::create_dir_all(&self.state_directory)?;
        let temporary = self.temporary_path();
        let mut file = create_private_file(&temporary)?;

        if let Err(error) = serde_json::to_writer_pretty(&mut file, identity) {
            let _ = fs::remove_file(&temporary);
            return Err(error.into());
        }
        if let Err(error) = file.write_all(b"\n").and_then(|_| file.sync_all()) {
            let _ = fs::remove_file(&temporary);
            return Err(error.into());
        }
        drop(file);

        match fs::rename(&temporary, &destination) {
            Ok(()) => Ok(()),
            Err(error) => {
                let _ = fs::remove_file(&temporary);
                if error.kind() == io::ErrorKind::AlreadyExists || destination.exists() {
                    Err(IdentityError::AlreadyExists { path: destination })
                } else {
                    Err(error.into())
                }
            }
        }
    }

    fn identity_path(&self) -> PathBuf {
        self.state_directory.join("identity.json")
    }

    fn temporary_path(&self) -> PathBuf {
        self.state_directory
            .join(format!(".identity-{}.tmp", Uuid::new_v4()))
    }
}

fn validate_schema_version(identity: &AgentIdentity) -> Result<(), IdentityError> {
    if identity.schema_version != 1 {
        return Err(IdentityError::UnsupportedSchemaVersion {
            version: identity.schema_version,
        });
    }
    Ok(())
}

fn create_private_file(path: &Path) -> io::Result<File> {
    let mut options = OpenOptions::new();
    options.write(true).create_new(true);

    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.mode(0o600);
    }

    options.open(path)
}

#[cfg(test)]
mod tests {
    use super::{AgentIdentity, IdentityError, IdentityStore};
    use std::fs;
    use tempfile::tempdir;
    use uuid::Uuid;

    fn identity() -> AgentIdentity {
        AgentIdentity {
            schema_version: 1,
            machine_id: Uuid::new_v4(),
            agent_id: Uuid::new_v4(),
            agent_secret: "secret-that-must-not-be-logged".to_owned(),
        }
    }

    #[test]
    fn missing_identity_returns_none() {
        let directory = tempdir().expect("create temporary directory");
        let store = IdentityStore::new(directory.path().to_path_buf());
        assert!(store.load().expect("load identity").is_none());
    }

    #[test]
    fn saves_and_loads_identity() {
        let directory = tempdir().expect("create temporary directory");
        let store = IdentityStore::new(directory.path().to_path_buf());
        let expected = identity();
        store.save_new(&expected).expect("save identity");
        let actual = store
            .load()
            .expect("load identity")
            .expect("identity should exist");

        assert_eq!(actual.schema_version, expected.schema_version);
        assert_eq!(actual.machine_id, expected.machine_id);
        assert_eq!(actual.agent_id, expected.agent_id);
        assert_eq!(actual.agent_secret, expected.agent_secret);
    }

    #[test]
    fn rejects_malformed_identity() {
        let directory = tempdir().expect("create temporary directory");
        fs::write(directory.path().join("identity.json"), "not JSON").expect("write state");
        let store = IdentityStore::new(directory.path().to_path_buf());
        assert!(matches!(store.load(), Err(IdentityError::Json(_))));
    }

    #[test]
    fn refuses_to_overwrite_identity() {
        let directory = tempdir().expect("create temporary directory");
        let store = IdentityStore::new(directory.path().to_path_buf());
        store.save_new(&identity()).expect("save first identity");
        assert!(matches!(
            store.save_new(&identity()),
            Err(IdentityError::AlreadyExists { .. })
        ));
    }

    #[test]
    fn rejects_unsupported_schema_version() {
        let directory = tempdir().expect("create temporary directory");
        let store = IdentityStore::new(directory.path().to_path_buf());
        let mut unsupported = identity();
        unsupported.schema_version = 2;
        assert!(matches!(
            store.save_new(&unsupported),
            Err(IdentityError::UnsupportedSchemaVersion { version: 2 })
        ));
    }
}
