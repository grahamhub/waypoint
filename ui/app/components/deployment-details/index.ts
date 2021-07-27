import Component from '@glimmer/component';
import { inject as service } from '@ember/service';
import { action } from '@ember/object';
import { tracked } from '@glimmer/tracking';
import ApiService from 'waypoint/services/api';
import { Deployment, Ref, ExpediteStatusReportRequest, GetJobStreamRequest, StatusReport } from 'waypoint-pb';
import { Status } from 'api-common-protos/google/rpc/status_pb';

interface DeploymentDetailsArgs {
  model: Deployment.AsObject & WithStatusReport;
}

interface WithStatusReport {
  statusReport?: StatusReport.AsObject;
}

export default class DeploymentDetails extends Component<DeploymentDetailsArgs> {
  @service api!: ApiService;
  @tracked isRefreshRunning = false;
  @tracked _statusReport?: StatusReport.AsObject;

  get statusReport() {
    return this._statusReport || this.args.model.statusReport;
  }

  @action
  async refreshHealthCheck(e: Event) {
    e.preventDefault();
    let ref = new Ref.Operation();
    //TODO (sabrinako): set the actual id we need to identify the operation
    ref.setId('stringid');

    let req = new ExpediteStatusReportRequest();
    req.setRef(ref);
    let resp = await this.api.expediteStatusReport(ref);

    if (resp?.id) {
      this.isRefreshRunning = true;
      let startTime = new Date(Date.now() - 20000);

      this._statusReport = {
        status: {
          completeTime: {
            seconds: Math.floor(startTime.getTime() / 1000),
            nanos: 0,
          },
          state: 1,
          details: 'detail',
        },
      };

      // let streamReq = new GetJobStreamRequest();
      // streamReq.setJobId(resp.id);
      // let jobStream = await this.api.client.getJobStream(streamReq);
    }
  }
}
