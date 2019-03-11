#!/usr/bin/env python
# encoding: utf-8

import os
import subprocess
import time

project_path = os.environ["GOPATH"]+"/src/github.com/ok-chain/okchain"
peer = project_path+"/build/bin/okchaincli"

ips = ["0.0.0."+str(suffix) for suffix in [0]] #[16, 21, 23, 24, 25]
ports = ["160"+str(port).zfill(2) for port in [14]] # range(1,21) -->
#ips = ["127.0.0.1"]
#ports = ["16008", "16014"]

testFrom1 = "0xbcf72fc47d4aeec195dba5e0e26cea2b75416ec4"
testFrom2 = "0x4ba6fa234186ff7db0c10faa103eecb02a4f26fb"
testTo ="0x84eaab7ecc07c333123a9a51976d596496d9e2a0"
testValue = 1
testPW = "okchain"
txsLoop = 100
time_interval = 1000
nonce = 0


#./peer account transfer --from "0xbcf72fc47d4aeec195dba5e0e26cea2b75416ec4" --to "3d651eb9ae5626260e2468d589aee62b86953046" --value 1 --pswd "okchain" --nodeaddr "http://192.168.168.68:16014"
def send_one_tx(fm, to, value, nonce, pw, nodeip):
    cmd = 'account transfer --from "{0}" --to "{1}" --amount {2} --nonce {3} --password "{4}" --url "{5}"'.format(fm, to, value, nonce, pw, "http://"+nodeip)
    print("./peer "+cmd)
    c = subprocess.Popen("{0} {1}".format(peer, cmd), shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    out, err = c.communicate()
    print(out)
    print(err)

def send_txs_to_nodes():
    for ip in ips:
        #send tx to len(ports) peers in one physical machine, such as: 16008, 16014
        for port in ports:
            for n in range(nonce, nonce+txsLoop):
                send_one_tx(testFrom1, testTo, testValue, n, testPW, "{0}:{1}".format(ip, port))
                send_one_tx(testFrom2, testTo, testValue, n, testPW, "{0}:{1}".format(ip, port))

#./peer account info --addr "这里是account地址" --url "http://127.0.0.1:16001"
def get_balance(fm, nodeip):
    cmd = 'account info --addr "{0}" --url "{1}"'.format(fm, "http://"+nodeip)
    print("./peer "+cmd)
    c = subprocess.Popen("{0} {1}".format(peer, cmd), shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    out, err = c.communicate()
    print(out) 
    print(err)

def TestMain():
    global nonce
    send_txs_to_nodes()    
    nonce += txsLoop
    time.sleep(time_interval)
    print("******************************************************************")
    print("\n========{0} txs send finished\n".format(nonce))
    print("\n")
    print("testFrom1:", get_balance(testFrom1, "{0}:{1}".format(ips[0], ports[0])))
    print("testFrom2:", get_balance(testFrom2, "{0}:{1}".format(ips[0], ports[0])))
    print("testTo:", get_balance(testTo, "{0}:{1}".format(ips[0], ports[0])))
    print("\n\n")

if __name__ == '__main__':
    while True:
        TestMain()
    




# def sendTransaction(pubkey='0x9b175d69a0A3236A1e6992d03FA0be0891D8D023', to='0xD8104E3E6dE811DD0cc07d32cCcE2f4f4B38403a', value=100    , nonce=0, ip='127.0.0.1:16014'):
#     method = '"method":"okchain_sendTransaction"'
#     # params = '"params":["'+pubkey+'","'+to+'",'+value+','+nonce+']'
#     params = '"params":["%s", "%s", %d, %d]'%(pubkey, to, value, nonce)
#     return cmd(method, params, ip)
# # //curl -X POST --data '{"jsonrpc":"2.0","method":"okchain_sendTransaction","params":["0x94dc66c8be8393e41791dfd7b8a47fe43f3e0890"    ,"0xA7f3dFed2BCF7b35A8824e11Ae8f723650EDFb58",1,1],"id":1}' -H "Content-Type: application/json" http://127.0.0.1:16014
