use thiserror::Error;

#[derive(Debug, Error)]
pub enum MachineMetadataError {
    #[error("collect hostname: {0}")]
    Hostname(#[from] std::io::Error),
    #[error("{field} must not be empty")]
    EmptyValue { field: &'static str },
}

#[derive(Debug, PartialEq, Eq)]
pub struct MachineMetadata {
    hostname: String,
    operating_system: String,
    os_version: String,
    architecture: String,
    agent_version: String,
    capabilities: Vec<String>,
}

pub fn collect() -> Result<MachineMetadata, MachineMetadataError> {
    let hostname = collect_hostname()?;
    let os_info = os_info::get();

    MachineMetadata::from_values(
        hostname,
        std::env::consts::OS.to_owned(),
        format!("{} {}", os_info.os_type(), os_info.version()),
        os_info
            .architecture()
            .unwrap_or(std::env::consts::ARCH)
            .to_string(),
        env!("CARGO_PKG_VERSION").to_owned(),
        Vec::new(),
    )
}

impl MachineMetadata {
    fn from_values(
        hostname: String,
        operating_system: String,
        os_version: String,
        architecture: String,
        agent_version: String,
        capabilities: Vec<String>,
    ) -> Result<Self, MachineMetadataError> {
        Ok(Self {
            hostname: required_value("hostname", hostname)?,
            operating_system: required_value("operating system", operating_system)?,
            os_version: required_value("OS version", os_version)?,
            architecture: required_value("architecture", architecture)?,
            agent_version: required_value("agent version", agent_version)?,
            capabilities,
        })
    }

    pub fn hostname(&self) -> &str {
        &self.hostname
    }

    pub fn operating_system(&self) -> &str {
        &self.operating_system
    }

    pub fn os_version(&self) -> &str {
        &self.os_version
    }

    pub fn architecture(&self) -> &str {
        &self.architecture
    }

    pub fn agent_version(&self) -> &str {
        &self.agent_version
    }

    pub fn capabilities(&self) -> &[String] {
        &self.capabilities
    }
}

fn collect_hostname() -> Result<String, MachineMetadataError> {
    Ok(hostname::get()?.to_string_lossy().into_owned())
}

fn required_value(field: &'static str, value: String) -> Result<String, MachineMetadataError> {
    let value = value.trim().to_owned();
    if value.is_empty() {
        return Err(MachineMetadataError::EmptyValue { field });
    }
    Ok(value)
}

#[cfg(test)]
mod tests {
    use super::{MachineMetadata, MachineMetadataError};

    fn metadata() -> MachineMetadata {
        MachineMetadata::from_values(
            " agent-01 ".to_owned(),
            " linux ".to_owned(),
            " Ubuntu 24.04 ".to_owned(),
            " x86_64 ".to_owned(),
            " 0.1.0 ".to_owned(),
            Vec::new(),
        )
        .expect("metadata should be valid")
    }

    #[test]
    fn normalizes_required_values() {
        let metadata = metadata();

        assert_eq!(metadata.hostname(), "agent-01");
        assert_eq!(metadata.operating_system(), "linux");
        assert_eq!(metadata.os_version(), "Ubuntu 24.04");
        assert_eq!(metadata.architecture(), "x86_64");
        assert_eq!(metadata.agent_version(), "0.1.0");
        assert!(metadata.capabilities().is_empty());
    }

    #[test]
    fn rejects_blank_required_values() {
        let error = MachineMetadata::from_values(
            " ".to_owned(),
            "linux".to_owned(),
            "Ubuntu 24.04".to_owned(),
            "x86_64".to_owned(),
            "0.1.0".to_owned(),
            Vec::new(),
        )
        .expect_err("metadata should be rejected");

        assert!(matches!(
            error,
            MachineMetadataError::EmptyValue { field: "hostname" }
        ));
    }
}
