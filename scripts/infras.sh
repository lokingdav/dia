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

    case "$network" in
        livenet)
            echo "Creating Cloud Infrastructure..."
            run_in_docker "cd $HOME_DIR && terraform apply"
            ;;
        *)
            echo "Available subcommands for create:"
            echo "create {livenet}"
            exit 1
            ;;
    esac
    
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

case "$1" in
    init)
        init
        ;;
    create)
        create $2
        ;;
    destroy)
        destroy
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