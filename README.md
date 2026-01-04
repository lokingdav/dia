# denseid-servers
Server implementations for DenseID

```bash
protoc --proto_path=api/proto \
    --go_out=api/go --go_opt=paths=source_relative \
    --go-grpc_out=api/go --go-grpc_opt=paths=source_relative \
    api/proto/enrollment/v1/enrollment.proto
```

```bash
# Infrastructure management
./scripts/infras.sh init              # Setup AWS & SSH keys
./scripts/infras.sh create            # Create EC2 instances
./scripts/infras.sh destroy           # Destroy infrastructure

# Playbook operations (simplified!)
./scripts/infras.sh play setup        # Full setup
./scripts/infras.sh play stop         # Stop services
./scripts/infras.sh play start        # Start services
./scripts/infras.sh play clear_logs   # Clear logs
./scripts/infras.sh play checkout_branch  # Pull latest code

# Combine tags with comma
./scripts/infras.sh play stop,clear_logs,start

# SSH access
./scripts/infras.sh ssh client-1
./scripts/infras.sh ssh server
```