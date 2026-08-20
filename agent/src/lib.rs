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
    let (identity, identity_source) = match store.load()? {
        Some(identity) => (identity, "existing"),
        None => {
            tracing::info!("agent identity is missing; starting enrollment");

            let machine_metadata = metadata::collect()?;
            tracing::info!(
                hostname = machine_metadata.hostname(),
                operating_system = machine_metadata.operating_system(),
                os_version = machine_metadata.os_version(),
                architecture = machine_metadata.architecture(),
                agent_version = machine_metadata.agent_version(),
                capability_count = machine_metadata.capabilities().len(),
                "collected machine metadata for enrollment"
            );

            tracing::info!("requesting enrollment from server");
            let enrolled_agent = enrollment::enroll(&config, &machine_metadata).await?;
            let identity = identity::AgentIdentity {
                schema_version: 1,
                machine_id: enrolled_agent.machine_id(),
                agent_id: enrolled_agent.agent_id(),
                agent_secret: enrolled_agent.agent_secret().to_string(),
            };

            tracing::info!(
                machine_id = %identity.machine_id,
                agent_id = %identity.agent_id,
                "received enrollment identity; saving local state"
            );
            store.save_new(&identity)?;

            (identity, "enrolled")
        }
    };

    tracing::info!(
        machine_id = %identity.machine_id,
        agent_id = %identity.agent_id,
        identity_source,
        "agent identity is ready"
    );

    tokio::signal::ctrl_c()
        .await
        .expect("the operating system should provide a shutdown signal");

    tracing::info!("agent shutdown requested");
    Ok(())
}
