#! /bin/bash

#
# ─── CONFIGS ────────────────────────────────────────────────────────────────────
#

#TODO

#
# ─── INSTALL VARIABLES ──────────────────────────────────────────────────────────
#

ARCH="$HOSTTYPE"
INSTALL_DIR="$HOME/PiWorker"
LATEST_URL="https://api.github.com/repos/Pegasus8/piworker/releases/latest"
SERVICE_ABSPATH="/etc/systemd/system/PiWorker.service"
SUPPORTED_ARCHS=("arm") 

#
# ─── STRINGS FORMATTING ─────────────────────────────────────────────────────────
#

BOLD='\e[1m'
HIGHLIGHTED='\e[7m'
BLUE_BACKGROUND='\e[48;2;0;34;204m'
GEEN_BACKGROUND='\e[48;2;45;114;20m'
RED_FOREGROUND='\e[38;2;160;43;51m'
RESET='\e[m'

print_blueb() {
    printf "${BOLD}${BLUE_BACKGROUND}$1${RESET}\n\n"
}

print_greenb() {
    printf "${BOLD}${GEEN_BACKGROUND}$1${RESET}\n\n"
}

print_redf() {
    printf "${BOLD}${RED_FOREGROUND}$1${RESET}\n\n"
}

#
# ─── INSTALLATION FUNCTIONS ─────────────────────────────────────────────────────
#

AddPath() {
    echo "export PATH=$PATH:$HOME/PiWorker/PiWorker" >>.bashrc
}

PrepareDirectory() {
    mkdir $INSTALL_DIR
}

DownloadLatest() {
    workdir="$(mktemp -d)"
    curl -sL $(curl -sL "$LATEST_URL" | grep PiWorker-linux_$ARCH- | grep browser_download_url | head -1 | cut -d \" -f 4) |
    tar xz -C "$workdir" -f - PiWorker && #FIXME Test if this line is correct. Must be after public release (due to url).
    [ -x $workdir/PiWorker ] &&
    mv -f "${workdir}/PiWorker" "${INSTALL_DIR}/" && 
    rm -fr "$workdir"
    
    # TODO Finish download section

    if [ -a $INSTALL_DIR/PiWorker ]; then
        print_greenb "PiWorker downloaded correctly!"
    else 
        print_redf "I can't download PiWorker"
    fi
}

InstallService() {
    print_blueb "Installing service..."
    $INSTALL_DIR/PiWorker --service install
    if [ -a $SERVICE_ABSPATH ]; then 
        print_greenb "Service installed!, starting it..."
        $INSTALL_DIR/PiWorker --service start
        print_greenb "Service started!"
    else 
        print_redf "The service was not installed :("
    fi
}

GenerateOpenSSLCertificate() {
    print_blueb "Generating a new self signed certificate..."
    openssl req \
        -subj '/O=PiWorker' \
        -new \
        -newkey \
        rsa:2048 \
        -sha256 \
        -days 365 \
        -nodes \
        -x509 \
        -keyout server.key \
        -out server.crt
    if [ -a $INSTALL_DIR/server.key ] && [ -a $INSTALL_DIR/server.crt ]; then
        print_greenb "Certificates generated successfully!"
    else 
        print_redf "The certificates could not be generated :("
    fi
}

InstallDependences() {
    # OpenSSL
    if hash openssl 2>/dev/null; then
        # OpenSSL installed, let's generate a self signed certificate
        print_greenb "OpenSSL already installed"
        GenerateOpenSSLCertificate
    else
        print_redf "OpenSSL not installed, installing it..."
        apt-get update && apt-get install -y openssl
        print_greenb "Done!"
        GenerateOpenSSLCertificate
    fi
    
}

#
# ─── EXECUTION ──────────────────────────────────────────────────────────────────
#

if [[ ! " ${SUPPORTED_ARCHS[@]} " =~ " ${ARCH} " ]]; then
    print_redf "For now, PiWorker doesn't support your architecture ($ARCH). Sorry."
    print_blueb "If you want PiWorker to support your architecture you can open an issue in the Github repository: https://github.com/Pegasus8/PiWorker/issues/new"
    exit 1
fi

if [[ $EUID -ne 0 ]]; then
   print_redf "This script must be run as root!" 
   exit 1
fi

InstallDependences
PrepareDirectory
DownloadLatest
InstallService
AddPath
