import Controller from '@ember/controller';
import { inject as service } from '@ember/service';
import { action } from '@ember/object';
import ApiService from 'waypoint/services/api';
import { Ref, ExpediteStatusReportRequest, GetJobStreamRequest } from 'waypoint-pb';

export default class WorkspaceProjectsProjectAppDeployment extends Controller {
  @service api!: ApiService;

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
      let streamReq = new GetJobStreamRequest();
      streamReq.setJobId(resp.id);
      await this.api.client.getJobStream(streamReq);
    }
  }
}

// DO NOT DELETE: this is how TypeScript knows how to look up your controllers.
declare module '@ember/controller' {
  interface Registry {
    'workspace/projects/project/app/deployment': WorkspaceProjectsProjectAppDeployment;
  }
}
