use std::{env, path::PathBuf};

use thiserror::Error;
use tonic::transport::Endpoint;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AgentConfig {
    pub server_endpoint: String,
    pub enrollment_token_file: Option<PathBuf>,
    pub state_directory: PathBuf,
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum ConfigError {
    #[error("{variable} must be set")]
    Missing { variable: &'static str },
    #[error("{variable} must not be empty")]
    Empty { variable: &'static str },
    #[error("ARXVEIL_AGENT_SERVER_ENDPOINT is invalid: {message}")]
    InvalidEndpoint { message: String },
    #[error("ARXVEIL_AGENT_SERVER_ENDPOINT must use http or https")]
    UnsupportedEndpointScheme,
}

impl AgentConfig {
    pub fn from_environment() -> Result<Self, ConfigError> {
        Self::from_values(
            env::var("ARXVEIL_AGENT_SERVER_ENDPOINT").ok(),
            env::var("ARXVEIL_AGENT_STATE_DIRECTORY").ok(),
            env::var("ARXVEIL_AGENT_ENROLLMENT_TOKEN_FILE").ok(),
        )
    }

    fn from_values(
        server_endpoint: Option<String>,
        state_directory: Option<String>,
        enrollment_token_file: Option<String>,
    ) -> Result<Self, ConfigError> {
        let server_endpoint = required_value("ARXVEIL_AGENT_SERVER_ENDPOINT", server_endpoint)?;
        let endpoint = Endpoint::from_shared(server_endpoint.clone()).map_err(|error| {
            ConfigError::InvalidEndpoint {
                message: error.to_string(),
            }
        })?;
        if !matches!(endpoint.uri().scheme_str(), Some("http" | "https")) {
            return Err(ConfigError::UnsupportedEndpointScheme);
        }

        let state_directory = required_value("ARXVEIL_AGENT_STATE_DIRECTORY", state_directory)?;
        let enrollment_token_file = enrollment_token_file
            .filter(|value| !value.trim().is_empty())
            .map(PathBuf::from);

        Ok(Self {
            server_endpoint,
            enrollment_token_file,
            state_directory: PathBuf::from(state_directory),
        })
    }
}

fn required_value(variable: &'static str, value: Option<String>) -> Result<String, ConfigError> {
    let value = value.ok_or(ConfigError::Missing { variable })?;
    if value.trim().is_empty() {
        return Err(ConfigError::Empty { variable });
    }
    Ok(value)
}

#[cfg(test)]
mod tests {
    use super::{AgentConfig, ConfigError};
    use std::path::PathBuf;

    #[test]
    fn accepts_valid_http_configuration() {
        let config = AgentConfig::from_values(
            Some("http://server:9090".to_owned()),
            Some("/var/lib/arxveil".to_owned()),
            Some("/bootstrap/token".to_owned()),
        )
        .expect("configuration should be valid");

        assert_eq!(config.server_endpoint, "http://server:9090");
        assert_eq!(config.state_directory, PathBuf::from("/var/lib/arxveil"));
        assert_eq!(
            config.enrollment_token_file,
            Some(PathBuf::from("/bootstrap/token"))
        );
    }

    #[test]
    fn accepts_https_without_an_enrollment_token_file() {
        let config = AgentConfig::from_values(
            Some("https://arxveil.example".to_owned()),
            Some("state".to_owned()),
            None,
        )
        .expect("configuration should be valid");

        assert_eq!(config.enrollment_token_file, None);
    }

    #[test]
    fn rejects_missing_endpoint() {
        let error = AgentConfig::from_values(None, Some("state".to_owned()), None)
            .expect_err("configuration should fail");
        assert_eq!(
            error,
            ConfigError::Missing {
                variable: "ARXVEIL_AGENT_SERVER_ENDPOINT"
            }
        );
    }

    #[test]
    fn rejects_blank_required_values() {
        let endpoint_error =
            AgentConfig::from_values(Some("  ".to_owned()), Some("state".to_owned()), None)
                .expect_err("blank endpoint should fail");
        assert_eq!(
            endpoint_error,
            ConfigError::Empty {
                variable: "ARXVEIL_AGENT_SERVER_ENDPOINT"
            }
        );

        let state_error = AgentConfig::from_values(
            Some("http://server:9090".to_owned()),
            Some(" ".to_owned()),
            None,
        )
        .expect_err("blank state directory should fail");
        assert_eq!(
            state_error,
            ConfigError::Empty {
                variable: "ARXVEIL_AGENT_STATE_DIRECTORY"
            }
        );
    }

    #[test]
    fn rejects_endpoint_without_http_or_https_scheme() {
        let error = AgentConfig::from_values(
            Some("server:9090".to_owned()),
            Some("state".to_owned()),
            None,
        )
        .expect_err("endpoint without an HTTP scheme should fail");
        assert_eq!(error, ConfigError::UnsupportedEndpointScheme);
    }

    #[test]
    fn rejects_unsupported_endpoint_scheme() {
        let error = AgentConfig::from_values(
            Some("ftp://server:9090".to_owned()),
            Some("state".to_owned()),
            None,
        )
        .expect_err("unsupported scheme should fail");
        assert_eq!(error, ConfigError::UnsupportedEndpointScheme);
    }
}
