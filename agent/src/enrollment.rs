use crate::{
    config::AgentConfig,
    metadata::MachineMetadata,
    proto::agent::v1::{EnrollRequest, EnrollResponse, agent_service_client::AgentServiceClient},
};
use thiserror::Error;
use uuid::Uuid;

#[derive(Debug, Error)]
pub enum EnrollmentError {
    #[error("connect to enrollment server: {0}")]
    Transport(#[from] tonic::transport::Error),

    #[error("enrollment RPC failed: {0}")]
    Status(#[from] tonic::Status),

    #[error("server returned an invalid machine ID: {0}")]
    InvalidMachineID(#[source] uuid::Error),

    #[error("server returned an invalid agent ID: {0}")]
    InvalidAgentID(#[source] uuid::Error),

    #[error("server returned an empty agent secret")]
    EmptySecret,
}

pub struct EnrolledAgent {
    machine_id: Uuid,
    agent_id: Uuid,
    agent_secret: String,
}

impl EnrolledAgent {
    pub fn machine_id(&self) -> Uuid {
        self.machine_id
    }

    pub fn agent_id(&self) -> Uuid {
        self.agent_id
    }

    pub fn agent_secret(&self) -> &str {
        &self.agent_secret
    }
}

pub async fn enroll(
    config: &AgentConfig,
    machine_metadata: &MachineMetadata,
) -> Result<EnrolledAgent, EnrollmentError> {
    let mut client = AgentServiceClient::connect(config.server_endpoint.clone()).await?;
    let request = EnrollRequest {
        hostname: machine_metadata.hostname().to_owned(),
        operating_system: machine_metadata.operating_system().to_owned(),
        os_version: machine_metadata.os_version().to_owned(),
        architecture: machine_metadata.architecture().to_owned(),
        agent_version: machine_metadata.agent_version().to_owned(),
        capabilities: machine_metadata.capabilities().to_owned(),
    };

    let response = client.enroll(request).await?.into_inner();
    enrolled_agent_from_response(response)
}

fn enrolled_agent_from_response(
    response: EnrollResponse,
) -> Result<EnrolledAgent, EnrollmentError> {
    if response.agent_secret.trim().is_empty() {
        return Err(EnrollmentError::EmptySecret);
    }

    Ok(EnrolledAgent {
        machine_id: Uuid::parse_str(&response.machine_id)
            .map_err(EnrollmentError::InvalidMachineID)?,
        agent_id: Uuid::parse_str(&response.agent_id).map_err(EnrollmentError::InvalidAgentID)?,
        agent_secret: response.agent_secret,
    })
}

#[cfg(test)]
mod tests {
    use super::{EnrollmentError, enrolled_agent_from_response};
    use crate::proto::agent::v1::EnrollResponse;
    use uuid::Uuid;

    fn response() -> EnrollResponse {
        EnrollResponse {
            machine_id: "ba491619-75cd-423c-b449-29f096f351cf".to_owned(),
            agent_id: "2f9a5441-82a0-4a76-b042-4854183394b0".to_owned(),
            agent_secret: "test-agent-secret".to_owned(),
        }
    }

    #[test]
    fn accepts_valid_enrollment_response() {
        let enrolled_agent =
            enrolled_agent_from_response(response()).expect("enrollment response should be valid");

        assert_eq!(
            enrolled_agent.machine_id,
            Uuid::parse_str("ba491619-75cd-423c-b449-29f096f351cf").expect("valid test UUID")
        );
        assert_eq!(
            enrolled_agent.agent_id,
            Uuid::parse_str("2f9a5441-82a0-4a76-b042-4854183394b0").expect("valid test UUID")
        );
        assert_eq!(enrolled_agent.agent_secret, "test-agent-secret");
    }

    #[test]
    fn rejects_invalid_machine_id() {
        let mut response = response();
        response.machine_id = "not-a-UUID".to_owned();

        assert!(matches!(
            enrolled_agent_from_response(response),
            Err(EnrollmentError::InvalidMachineID(_))
        ));
    }

    #[test]
    fn rejects_invalid_agent_id() {
        let mut response = response();
        response.agent_id = "not-a-UUID".to_owned();

        assert!(matches!(
            enrolled_agent_from_response(response),
            Err(EnrollmentError::InvalidAgentID(_))
        ));
    }

    #[test]
    fn rejects_blank_agent_secret() {
        let mut response = response();
        response.agent_secret = " ".to_owned();

        assert!(matches!(
            enrolled_agent_from_response(response),
            Err(EnrollmentError::EmptySecret)
        ));
    }
}
