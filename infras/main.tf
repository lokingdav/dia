terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 4.16"
    }
  }
  required_version = ">= 1.2.0"
}

# Ubuntu AMI map for different regions
variable "ubuntu_ami_map" {
  type = map(string)
  default = {
    "us-east-1" = "ami-04b4f1a9cf54c11d0"
    "us-east-2" = "ami-0cb91c7de36eed2cb"
    "us-west-1" = "ami-07d2649d67dbe8900"
    "us-west-2" = "ami-05134c8ef96964280"
  }
}

# Instance type for standard spec instances (2 instances)
variable "standard_instance_type" {
  description = "Instance type for standard spec EC2 instances"
  type        = string
  default     = "t3.small"
}

# Instance type for high spec instance (1 instance)
variable "high_spec_instance_type" {
  description = "Instance type for high spec EC2 instance"
  type        = string
  default     = "t3.small"
}

# Regions for the 3 instances
variable "region_1" {
  description = "First region for client instance (West Coast)"
  type        = string
  default     = "us-west-1"
}

variable "region_2" {
  description = "Second region for client instance (East Coast)"
  type        = string
  default     = "us-east-1"
}

variable "region_3" {
  description = "Third region for server (Central US)"
  type        = string
  default     = "us-east-2"
}

variable "region_4" {
  description = "Fourth region for Asterisk server"
  type        = string
  default     = "us-west-2"
}

# Enable/disable instances
variable "enable_client_1" {
  description = "Enable client-1 instance"
  type        = bool
  default     = false
}

variable "enable_client_2" {
  description = "Enable client-2 instance"
  type        = bool
  default     = false
}

variable "enable_server" {
  description = "Enable relay server instance"
  type        = bool
  default     = true
}

variable "enable_asterisk" {
  description = "Enable Asterisk server instance"
  type        = bool
  default     = true
}

variable "key_name" {
  default = "dia-keypair"
}

variable "sg_start_port" {
  default = 50051
}

variable "sg_end_port" {
  default = 50055
}

# AWS Providers for 4 regions
provider "aws" {
  alias  = "region1"
  region = var.region_1
}

provider "aws" {
  alias  = "region2"
  region = var.region_2
}

provider "aws" {
  alias  = "region3"
  region = var.region_3
}

provider "aws" {
  alias  = "region4"
  region = var.region_4
}

# SSH Key Pairs
resource "aws_key_pair" "region1" {
  count      = var.enable_client_1 ? 1 : 0
  provider   = aws.region1
  key_name   = var.key_name
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "aws_key_pair" "region2" {
  count      = var.enable_client_2 ? 1 : 0
  provider   = aws.region2
  key_name   = var.key_name
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "aws_key_pair" "region3" {
  count      = var.enable_server ? 1 : 0
  provider   = aws.region3
  key_name   = var.key_name
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "aws_key_pair" "region4" {
  count      = var.enable_asterisk ? 1 : 0
  provider   = aws.region4
  key_name   = var.key_name
  public_key = file("~/.ssh/id_ed25519.pub")
}

# Security Groups
resource "aws_security_group" "sg_region1" {
  count       = var.enable_client_1 ? 1 : 0
  provider    = aws.region1
  name_prefix = "dia-sg-${var.region_1}-"

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  ingress {
    from_port   = var.sg_start_port
    to_port     = var.sg_end_port
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  ingress {
    from_port   = 5060
    to_port     = 5060
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "SIP"
  }
  ingress {
    from_port   = 5060
    to_port     = 5060
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "SIP"
  }
  ingress {
    from_port   = 4444
    to_port     = 4444
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Baresip ctrl_tcp"
  }
  ingress {
    from_port   = 10000
    to_port     = 20000
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "RTP media"
  }  
  ingress {
    from_port   = 10000
    to_port     = 20000
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "RTP media"
  }  
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "sg_region2" {
  count       = var.enable_client_2 ? 1 : 0
  provider    = aws.region2
  name_prefix = "dia-sg-${var.region_2}-"

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  ingress {
    from_port   = var.sg_start_port
    to_port     = var.sg_end_port
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  ingress {
    from_port   = 5060
    to_port     = 5060
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "SIP"
  }
  ingress {
    from_port   = 5060
    to_port     = 5060
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "SIP"
  }
  ingress {
    from_port   = 4444
    to_port     = 4444
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Baresip ctrl_tcp"
  }
  ingress {
    from_port   = 10000
    to_port     = 20000
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "RTP media"
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "sg_region3" {
  count       = var.enable_server ? 1 : 0
  provider    = aws.region3
  name_prefix = "dia-sg-${var.region_3}-"

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  ingress {
    from_port   = var.sg_start_port
    to_port     = var.sg_end_port
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  ingress {
    from_port   = 5060
    to_port     = 5060
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "SIP"
  }
  ingress {
    from_port   = 5060
    to_port     = 5060
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "SIP"
  }
  ingress {
    from_port   = 4444
    to_port     = 4444
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Baresip ctrl_tcp"
  }
  ingress {
    from_port   = 10000
    to_port     = 20000
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "RTP media"
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "sg_region4" {
  count       = var.enable_asterisk ? 1 : 0
  provider    = aws.region4
  name_prefix = "dia-sg-${var.region_4}-"

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  ingress {
    from_port   = 5060
    to_port     = 5060
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "SIP"
  }
  ingress {
    from_port   = 5060
    to_port     = 5060
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "SIP"
  }
  ingress {
    from_port   = 10000
    to_port     = 20000
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "RTP media"
  }
  ingress {
    from_port   = 5038
    to_port     = 5038
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Asterisk AMI"
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# EC2 Instances
# Instance 1 - Client (West Coast)
resource "aws_instance" "instance_1" {
  count           = var.enable_client_1 ? 1 : 0
  provider        = aws.region1
  ami             = var.ubuntu_ami_map[var.region_1]
  instance_type   = var.standard_instance_type
  key_name        = aws_key_pair.region1[0].key_name
  security_groups = [aws_security_group.sg_region1[0].name]

  user_data = file("../${path.module}/scripts/setup-instance.sh")

  tags = {
    Name = "client-1"
    Type = "client"
  }
}

# Instance 2 - Client (East Coast)
resource "aws_instance" "instance_2" {
  count           = var.enable_client_2 ? 1 : 0
  provider        = aws.region2
  ami             = var.ubuntu_ami_map[var.region_2]
  instance_type   = var.standard_instance_type
  key_name        = aws_key_pair.region2[0].key_name
  security_groups = [aws_security_group.sg_region2[0].name]

  user_data = file("../${path.module}/scripts/setup-instance.sh")

  tags = {
    Name = "client-2"
    Type = "client"
  }
}

# Instance 3 - Server (Central)
resource "aws_instance" "instance_3" {
  count           = var.enable_server ? 1 : 0
  provider        = aws.region3
  ami             = var.ubuntu_ami_map[var.region_3]
  instance_type   = var.high_spec_instance_type
  key_name        = aws_key_pair.region3[0].key_name
  security_groups = [aws_security_group.sg_region3[0].name]

  user_data = file("../${path.module}/scripts/setup-instance.sh")

  tags = {
    Name = "server"
    Type = "server"
  }
}

# Instance 4 - Asterisk Server
resource "aws_instance" "instance_4" {
  count           = var.enable_asterisk ? 1 : 0
  provider        = aws.region4
  ami             = var.ubuntu_ami_map[var.region_4]
  instance_type   = var.standard_instance_type
  key_name        = aws_key_pair.region4[0].key_name
  security_groups = [aws_security_group.sg_region4[0].name]

  user_data = file("../${path.module}/scripts/setup-instance.sh")

  tags = {
    Name = "asterisk"
    Type = "asterisk"
  }
}

# Outputs
# Outputs
output "public_ips" {
  value = compact(concat(
    var.enable_client_1 ? [aws_instance.instance_1[0].public_ip] : [],
    var.enable_client_2 ? [aws_instance.instance_2[0].public_ip] : [],
    var.enable_server ? [aws_instance.instance_3[0].public_ip] : [],
    var.enable_asterisk ? [aws_instance.instance_4[0].public_ip] : []
  ))
}

output "private_ips" {
  value = compact(concat(
    var.enable_client_1 ? [aws_instance.instance_1[0].private_ip] : [],
    var.enable_client_2 ? [aws_instance.instance_2[0].private_ip] : [],
    var.enable_server ? [aws_instance.instance_3[0].private_ip] : [],
    var.enable_asterisk ? [aws_instance.instance_4[0].private_ip] : []
  ))
}

output "instance_details" {
  value = merge(
    var.enable_client_1 ? {
      instance_1 = {
        name          = aws_instance.instance_1[0].tags.Name
        public_ip     = aws_instance.instance_1[0].public_ip
        private_ip    = aws_instance.instance_1[0].private_ip
        instance_type = aws_instance.instance_1[0].instance_type
        region        = var.region_1
      }
    } : {},
    var.enable_client_2 ? {
      instance_2 = {
        name          = aws_instance.instance_2[0].tags.Name
        public_ip     = aws_instance.instance_2[0].public_ip
        private_ip    = aws_instance.instance_2[0].private_ip
        instance_type = aws_instance.instance_2[0].instance_type
        region        = var.region_2
      }
    } : {},
    var.enable_server ? {
      instance_3 = {
        name          = aws_instance.instance_3[0].tags.Name
        public_ip     = aws_instance.instance_3[0].public_ip
        private_ip    = aws_instance.instance_3[0].private_ip
        instance_type = aws_instance.instance_3[0].instance_type
        region        = var.region_3
      }
    } : {},
    var.enable_asterisk ? {
      instance_4 = {
        name          = aws_instance.instance_4[0].tags.Name
        public_ip     = aws_instance.instance_4[0].public_ip
        private_ip    = aws_instance.instance_4[0].private_ip
        instance_type = aws_instance.instance_4[0].instance_type
        region        = var.region_4
      }
    } : {}
  )
}

# Ansible hosts file
locals {
  hosts_yaml = join("\n", concat(
    var.enable_client_1 ? [
      "    ${aws_instance.instance_1[0].tags.Name}:",
      "      ansible_host: ${aws_instance.instance_1[0].public_ip}",
      "      ansible_user: ubuntu",
      "      type: client",
      "      instance_type: ${aws_instance.instance_1[0].instance_type}",
      "      region: ${var.region_1}"
    ] : [],
    var.enable_client_2 ? [
      "    ${aws_instance.instance_2[0].tags.Name}:",
      "      ansible_host: ${aws_instance.instance_2[0].public_ip}",
      "      ansible_user: ubuntu",
      "      type: client",
      "      instance_type: ${aws_instance.instance_2[0].instance_type}",
      "      region: ${var.region_2}"
    ] : [],
    var.enable_server ? [
      "    ${aws_instance.instance_3[0].tags.Name}:",
      "      ansible_host: ${aws_instance.instance_3[0].public_ip}",
      "      ansible_user: ubuntu",
      "      type: server",
      "      instance_type: ${aws_instance.instance_3[0].instance_type}",
      "      region: ${var.region_3}"
    ] : [],
    var.enable_asterisk ? [
      "    ${aws_instance.instance_4[0].tags.Name}:",
      "      ansible_host: ${aws_instance.instance_4[0].public_ip}",
      "      ansible_user: ubuntu",
      "      type: asterisk",
      "      instance_type: ${aws_instance.instance_4[0].instance_type}",
      "      region: ${var.region_4}"
    ] : []
  ))
}

resource "local_file" "ansible_hosts" {
  content  = "all:\n  hosts:\n${local.hosts_yaml}\n"
  filename = "./hosts.yml"
}
