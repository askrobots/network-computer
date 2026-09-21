#!/bin/sh
# Create an EC2 instance and provision it.
# Needs: awscli v2 configured (aws configure).
# Default region us-east-2 (Ohio, ~25-30ms from Houston, simplest).
# For the Houston Local Zone (single-digit ms) see the notes in infra/README.md.
# Usage: infra/create-aws.sh [name] [region] [type]
set -e
NAME=${1:-nc-ohio}; REGION=${2:-us-east-2}; TYPE=${3:-t3.medium}
DIR=$(dirname "$0")
command -v aws >/dev/null || { echo "install awscli: brew install awscli && aws configure"; exit 1; }

echo "resolving latest Ubuntu 24.04 AMI in $REGION..."
AMI=$(aws ec2 describe-images --region "$REGION" --owners 099720109477 \
  --filters "Name=name,Values=ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*" \
  --query 'sort_by(Images,&CreationDate)[-1].ImageId' --output text)
[ "$AMI" != None ] || { echo "no Ubuntu 24.04 AMI found"; exit 1; }

# import the local SSH key
KEYNAME=nc-$(hostname -s)
aws ec2 import-key-pair --region "$REGION" --key-name "$KEYNAME" \
  --public-key-material "fileb://$HOME/.ssh/id_ed25519.pub" 2>/dev/null || true

# security group with the ports the host needs
SG=$(aws ec2 create-security-group --region "$REGION" --group-name nc-$NAME \
  --description "network-computer" --query GroupId --output text 2>/dev/null || \
  aws ec2 describe-security-groups --region "$REGION" --group-names nc-$NAME --query 'SecurityGroups[0].GroupId' --output text)
for r in "tcp 22" "tcp 8765" "udp 3478" "udp 49152-65535"; do
  set -- $r
  aws ec2 authorize-security-group-ingress --region "$REGION" --group-id "$SG" \
    --ip-permissions "IpProtocol=$1,FromPort=${2%-*},ToPort=${2#*-},IpRanges=[{CidrIp=0.0.0.0/0}]" 2>/dev/null || true
done

echo "launching $TYPE from $AMI..."
IID=$(aws ec2 run-instances --region "$REGION" --image-id "$AMI" --instance-type "$TYPE" \
  --key-name "$KEYNAME" --security-group-ids "$SG" \
  --block-device-mappings 'DeviceName=/dev/sda1,Ebs={VolumeSize=30}' \
  --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=$NAME}]" \
  --query 'Instances[0].InstanceId' --output text)
echo "instance $IID, waiting for public ip..."
aws ec2 wait instance-running --region "$REGION" --instance-ids "$IID"
IP=$(aws ec2 describe-instances --region "$REGION" --instance-ids "$IID" \
  --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)
echo "ip: $IP"
sh "$DIR/provision-host.sh" "$IP" "$NAME" ubuntu
