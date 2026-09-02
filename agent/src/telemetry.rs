use std::time::SystemTime;

use sysinfo::{Disks, System};

#[derive(Debug, PartialEq)]
pub(crate) struct TelemetrySample {
    pub(crate) collected_at: SystemTime,
    pub(crate) cpu_usage_percent: f64,
    pub(crate) memory_total_bytes: i64,
    pub(crate) memory_used_bytes: i64,
    pub(crate) disk_total_bytes: i64,
    pub(crate) disk_used_bytes: i64,
    pub(crate) uptime_seconds: i64,
}

#[derive(Debug)]
pub(crate) struct TelemetryCollector {
    system: System,
    disks: Disks,
}

#[derive(Debug, thiserror::Error)]
pub(crate) enum TelemetryError {
    #[error("telemetry field {field} exceeds the supported integer range")]
    OutOfRange { field: &'static str },

    #[error("invalid telemetry field {field}: {reason}")]
    InvalidMeasurement {
        field: &'static str,
        reason: &'static str,
    },

    #[error("telemetry field {field} is unavailable")]
    Unavailable { field: &'static str },
}

impl TelemetryCollector {
    pub(crate) fn new() -> Self {
        let mut system = System::new();
        system.refresh_cpu_usage();

        let disks = Disks::new_with_refreshed_list();

        Self { system, disks }
    }

    pub(crate) fn collect(&mut self) -> Result<TelemetrySample, TelemetryError> {
        self.system.refresh_cpu_usage();
        self.system.refresh_memory();
        self.disks.refresh(true);

        if self.system.cpus().is_empty() {
            return Err(TelemetryError::Unavailable {
                field: "cpu_usage_percent",
            });
        }

        let cpu_usage_percent = f64::from(self.system.global_cpu_usage());
        if !cpu_usage_percent.is_finite() || !(0.0..=100.0).contains(&cpu_usage_percent) {
            return Err(TelemetryError::InvalidMeasurement {
                field: "cpu_usage_percent",
                reason: "must be finite and between 0 and 100",
            });
        }

        let memory_total_bytes = self.system.total_memory();
        let memory_used_bytes = self.system.used_memory();
        if memory_total_bytes == 0 {
            return Err(TelemetryError::Unavailable {
                field: "memory_total_bytes",
            });
        }
        if memory_used_bytes > memory_total_bytes {
            return Err(TelemetryError::InvalidMeasurement {
                field: "memory_used_bytes",
                reason: "must not exceed total memory",
            });
        }

        let disks = self.disks.list();
        if disks.is_empty() {
            return Err(TelemetryError::Unavailable {
                field: "disk_space",
            });
        }

        let mut disk_total_bytes = 0_u64;
        let mut disk_used_bytes = 0_u64;
        for disk in disks {
            let total = disk.total_space();
            let available = disk.available_space();
            let used = total
                .checked_sub(available)
                .ok_or(TelemetryError::InvalidMeasurement {
                    field: "disk_used_bytes",
                    reason: "available disk space must not exceed total disk space",
                })?;

            disk_total_bytes =
                disk_total_bytes
                    .checked_add(total)
                    .ok_or(TelemetryError::OutOfRange {
                        field: "disk_total_bytes",
                    })?;
            disk_used_bytes =
                disk_used_bytes
                    .checked_add(used)
                    .ok_or(TelemetryError::OutOfRange {
                        field: "disk_used_bytes",
                    })?;
        }

        let memory_total_bytes =
            i64::try_from(memory_total_bytes).map_err(|_| TelemetryError::OutOfRange {
                field: "memory_total_bytes",
            })?;
        let memory_used_bytes =
            i64::try_from(memory_used_bytes).map_err(|_| TelemetryError::OutOfRange {
                field: "memory_used_bytes",
            })?;
        let disk_total_bytes =
            i64::try_from(disk_total_bytes).map_err(|_| TelemetryError::OutOfRange {
                field: "disk_total_bytes",
            })?;
        let disk_used_bytes =
            i64::try_from(disk_used_bytes).map_err(|_| TelemetryError::OutOfRange {
                field: "disk_used_bytes",
            })?;
        let uptime_seconds =
            i64::try_from(System::uptime()).map_err(|_| TelemetryError::OutOfRange {
                field: "uptime_seconds",
            })?;

        Ok(TelemetrySample {
            collected_at: SystemTime::now(),
            cpu_usage_percent,
            memory_total_bytes,
            memory_used_bytes,
            disk_total_bytes,
            disk_used_bytes,
            uptime_seconds,
        })
    }
}
