#!/bin/bash

set -e

MAIN_HOSTS_FILE=hosts.yml
CONTROL_IMAGE="dia-ctrl"
HOME_DIR="infras"

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
    local network=$1

    # if main main.yml exists, notify and exit with error
    hosts_file="infras/$MAIN_HOSTS_FILE"
    if [ -f "$hosts_file" ]; then
        echo "Error: $hosts_file already exists. Please run 'infras destroy' to destroy the current setup before creating a new infrastructure."
        exit 1
    fi

    echo "Creating Cloud Infrastructure..."
    run_in_docker "cd $HOME_DIR && terraform apply"
    echo "Infrastructure created successfully."
}

destroy() {
    echo "Destroying Cloud Infrastructure (if applicable)..."
    run_in_docker "cd $HOME_DIR && terraform destroy && rm -rf $MAIN_HOSTS_FILE"
    echo "Destroyed all resources successfully."
}

reset() {
    echo "Resetting infrastructure..."
    destroy
}

install() {
    pull="$1"
    run_in_docker "cd $HOME_DIR && ansible-playbook -i $MAIN_HOSTS_FILE install.yml $pull"
}

pull_changes() {
    install "--tags 'checkout_branch'"
}

runapp() {
    local app=$1
    local pull=$2
    local directly=$3

    tags="stop_services,clear_logs"
    if [ "$pull" == "--pull" ]; then
        tags="$tags,checkout_branch"
    fi

    case "$app" in
        jodi)
            tags="$tags,start_jodi"
            ;;
        oobss)
            tags="$tags,start_oobss"
            ;;
        *)
            echo "Unknown app: $app"
            echo "Usage: infras run {jodi|oobss}"
            exit 1
            ;;
    esac

    local cmd_str="cd $HOME_DIR && ansible-playbook -i $MAIN_HOSTS_FILE playbooks/main.yml --tags $tags"
    
    if [ "$directly" == "--directly" ]; then
        eval "$cmd_str"
    else
        run_in_docker "$cmd_str"
    fi
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
    ssh)
        infrassh $2
        ;;
    reset)
        reset
        ;;
    install)
        install
        ;;
    pull)
        pull_changes
        ;;
    run)
        runapp "$2" "$3" "$4"
        ;;
    *)
        echo "Usage: infras {init|create|destroy|install|pull|run} [args...]"
        exit 1
        ;;
esac