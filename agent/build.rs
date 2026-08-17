use std::{env, error::Error, path::PathBuf};

fn main() -> Result<(), Box<dyn Error>> {
    let proto_root = PathBuf::from("../proto");
    let agent_proto = proto_root.join("agent/v1/agent.proto");

    println!("cargo::rerun-if-changed={}", agent_proto.display());

    let protoc = protoc_bin_vendored::protoc_bin_path()?;
    let protobuf_include = protoc_bin_vendored::include_path()?;

    unsafe {
        env::set_var("PROTOC", protoc);
    }

    tonic_prost_build::configure()
        .build_client(true)
        .build_server(false)
        .compile_protos(&[agent_proto], &[proto_root, protobuf_include])?;

    Ok(())
}
