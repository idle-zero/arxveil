#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "arxveil_agent=info".into()),
        )
        .with_target(false)
        .init();

    if let Err(error) = arxveil_agent::run().await {
        tracing::error!(error = %error, "agent stopped with an error");
        std::process::exit(1);
    }
}
