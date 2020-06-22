FROM google/cloud-sdk:slim

RUN mkdir /gcpviz
ADD gcpviz.py requirements.txt solarized-style.yaml wait_for_export.sh /gcpviz/
RUN chmod +x /gcpviz/gcpviz.py /gcpviz/wait_for_export.sh
RUN cd /gcpviz && pip3 install -r requirements.txt

RUN apt-get update
RUN apt-get install -y graphviz