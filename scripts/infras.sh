#!/bin/bash

set -e

MAIN_HOSTS_FILE=hosts.yml
CONTROL_IMAGE="dia-ctrl"
HOME_DIR="infras"
PLAYBOOK="playbook.yml"

run_in_docker() {
    local command=$1
    local app_dir=$(pwd)

    docker run -it --rm \
        -v "$app_dir:/app" \
        -v "$app_dir/docker/data/.aws:/root/.aws" \
        -v "$app_dir/docker/data/.ssh:/root/.ssh" \
        -e ANSIBLE_HOST_KEY_CHECKING=False \
        -e ANSIBLE_CONFIG=/app/$HOME_DIR/ansible.cfg \
        "$CONTROL_IMAGE" \
        /bin/bash -c "cd /app && $command"
}

init() {
    run_in_docker "./scripts/configure-aws-cli.sh"
}

create() {
    hosts_file="infras/$MAIN_HOSTS_FILE"
    if [ -f "$hosts_file" ]; then
        echo "Error: $hosts_file already exists. Please run 'infras destroy' first."
        exit 1
    fi

    echo "Creating Cloud Infrastructure..."
    run_in_docker "cd $HOME_DIR && terraform apply"
    echo "Infrastructure created successfully."
}

destroy() {
    echo "Destroying Cloud Infrastructure..."
    run_in_docker "cd $HOME_DIR && terraform destroy && rm -rf $MAIN_HOSTS_FILE"
    echo "Destroyed all resources successfully."
}

play() {
    local tags=$1
    
    if [ -z "$tags" ]; then
        echo "Error: Please specify tags (setup, stop, start, clear_logs, checkout_branch)"
        exit 1
    fi
    
    hosts_file="infras/$MAIN_HOSTS_FILE"
    if [ ! -f "$hosts_file" ]; then
        echo "Error: $hosts_file not found. Please run './scripts/infras.sh create' first."
        exit 1
    fi
    
    echo "Running playbook with tags: $tags"
    run_in_docker "cd $HOME_DIR && ansible-playbook -i $MAIN_HOSTS_FILE $PLAYBOOK --tags $tags"
}

infrassh() {
    local instance=$1
    
    if [ -z "$instance" ]; then
        echo "Error: Please specify an instance name (client-1, client-2, or server)"
        exit 1
    fi
    
    hosts_file="infras/$MAIN_HOSTS_FILE"
    if [ ! -f "$hosts_file" ]; then
        echo "Error: $hosts_file not found. Please run 'infras create' first."
        exit 1
    fi
    
    # Extract the host IP from the hosts.yml file
    host_ip=$(grep -A 1 "^    $instance:" "$hosts_file" | grep "ansible_host:" | awk '{print $2}')
    
    if [ -z "$host_ip" ]; then
        echo "Error: Instance '$instance' not found in $hosts_file"
        echo "Available instances:"
        grep -E "^    [a-z0-9-]+:" "$hosts_file" | sed 's/://g' | awk '{print "  - " $1}'
        exit 1
    fi
    
    # Get the script's directory to build absolute path
    local script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    local project_dir="$(dirname "$script_dir")"
    local ssh_dir="$project_dir/docker/data/.ssh"
    local ssh_key="$ssh_dir/id_ed25519"

    echo "FILE: $ssh_key"
    
    # Fix SSH directory and key permissions (owned by root from Docker)
    if sudo test -f "$ssh_key"; then
        echo "Fixing SSH key permissions..."
        sudo chown -R $USER:$USER "$ssh_dir"
        chmod 700 "$ssh_dir"
        chmod 600 "$ssh_key"
        chmod 644 "$ssh_key.pub" 2>/dev/null || true
    else
        echo "Error: SSH key not found at $ssh_key"
        echo "Run './scripts/infras.sh init' to generate SSH keys."
        exit 1
    fi
    
    echo "Connecting to $instance ($host_ip)..."
    ssh -i "$ssh_key" -o StrictHostKeyChecking=no ubuntu@$host_ip
}

case "$1" in
    init)
        init
        ;;
    create)
        create
        ;;
    destroy)
        destroy
        ;;
    play)
        play "$2"
        ;;
    ssh)
        infrassh "$2"
        ;;
    *)
        echo "Usage: $0 {init|create|destroy|play <tags>|ssh <instance>}"
        echo ""
        echo "Commands:"
        echo "  init              - Configure AWS CLI and generate SSH keys"
        echo "  create            - Create cloud infrastructure with Terraform"
        echo "  destroy           - Destroy cloud infrastructure"
        echo "  play <tags>       - Run Ansible playbook with specified tags"
        echo "  ssh <instance>    - SSH into instance (client-1, client-2, server)"
        echo ""
        echo "Playbook tags:"
        echo "  setup             - Full setup (wait, clone, pull images, baresip)"
        echo "  stop              - Stop Docker services only"
        echo "  start             - Start Docker services only"
        echo "  restart_services  - Restart Docker services (stop + start)"
        echo "  stop_baresip      - Stop baresip on clients"
        echo "  start_baresip     - Start baresip on clients"
        echo "  restart_baresip   - Restart baresip (stop + start)"
        echo "  clear_logs        - Clear application and Docker logs"
        echo "  checkout_branch   - Update to latest branch"
        echo ""
        echo "Examples:"
        echo "  ./scripts/infras.sh play setup"
        echo "  ./scripts/infras.sh play stop"
        echo "  ./scripts/infras.sh play start"
        echo "  ./scripts/infras.sh play restart_services"
        echo "  ./scripts/infras.sh play stop_baresip"
        echo "  ./scripts/infras.sh play restart_baresip"
        echo "  ./scripts/infras.sh ssh server"
        exit 1
        ;;
esac