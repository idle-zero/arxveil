pub mod config;
pub mod identity;
pub mod proto;

use thiserror::Error;

#[derive(Debug, Error)]
pub enum AgentError {
    #[error(transparent)]
    Config(#[from] config::ConfigError),

    #[error(transparent)]
    Identity(#[from] identity::IdentityError),
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
            tracing::info!(
                token_file_configured = config.enrollment_token_file.is_some(),
                "agent has no identity and will require enrollment"
            );
        }
    }

    tokio::signal::ctrl_c()
        .await
        .expect("the operating system should provide a shutdown signal");

    tracing::info!("agent shutdown requested");
    Ok(())
}
