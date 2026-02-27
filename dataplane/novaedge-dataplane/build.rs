fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Proto file will be added in Phase 1.2; skip if missing.
    let proto = "../proto/dataplane.proto";
    if std::path::Path::new(proto).exists() {
        tonic_build::compile_protos(proto)?;
    }
    Ok(())
}
