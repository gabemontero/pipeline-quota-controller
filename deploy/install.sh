#!/bin/sh

echo "Executing install.sh"

echo "pipelinerun quota controller image:"
echo ${PIPELINERUN_QUOTA_CONTROLLER_IMAGE}

DIR=`dirname $0`
echo "Running out of ${DIR}"
$DIR/install-openshift-pipelines.sh
find $DIR -name development -exec rm -r {} \;
find $DIR -name dev-template -exec cp -r {} {}/../development \;
find $DIR -path \*development\*.yaml -exec sed -i s%pipelinerun-quota-controller-image%${PIPELINERUN_QUOTA_CONTROLLER_IMAGE}% {} \;
find $DIR -path \*development\*.yaml -exec sed -i s/dev-template/development/ {} \;
oc apply -k $DIR/operator/overlay/development