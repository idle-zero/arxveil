pub mod config;
pub mod enrollment;
pub mod identity;
pub mod metadata;
pub mod proto;

use thiserror::Error;

#[derive(Debug, Error)]
pub enum AgentError {
    #[error(transparent)]
    Config(#[from] config::ConfigError),

    #[error(transparent)]
    Identity(#[from] identity::IdentityError),

    #[error(transparent)]
    Metadata(#[from] metadata::MachineMetadataError),

    #[error(transparent)]
    Enrollment(#[from] enrollment::EnrollmentError),
}

pub async fn run() -> Result<(), AgentError> {
    let config = config::AgentConfig::from_environment()?;
    let store = identity::IdentityStore::new(config.state_directory.clone());
    match store.load()? {
        Some(identity) => {
            tracing::info!(
                machine_id = %identity.machine_id,
                agent_id = %identity.agent_id,
                "loaded enrolled agent identity"
            );
        }
        None => {
            tracing::info!("agent has no identity");
            let machine_metadata = metadata::collect()?;

            let enrolled_agent = enrollment::enroll(&config, &machine_metadata).await?;
            store.save_new(&identity::AgentIdentity {
                schema_version: 1,
                machine_id: enrolled_agent.machine_id(),
                agent_id: enrolled_agent.agent_id(),
                agent_secret: enrolled_agent.agent_secret().to_string(),
            })?;
        }
    }

    tokio::signal::ctrl_c()
        .await
        .expect("the operating system should provide a shutdown signal");

    tracing::info!("agent shutdown requested");
    Ok(())
}
