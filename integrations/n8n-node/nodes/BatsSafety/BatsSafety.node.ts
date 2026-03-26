import {
  IExecuteFunctions,
  INodeExecutionData,
  INodeType,
  INodeTypeDescription,
} from 'n8n-workflow';

import axios from 'axios';

export class BatsSafety implements INodeType {
  description: INodeTypeDescription = {
    displayName: 'BATS Safety Gate',
    name: 'batsSafety',
    icon: 'file:bats.svg',
    group: ['transform'],
    version: 1,
    description: 'BATS Byzantine Safety Gate for AI agents',
    defaults: {
      name: 'BATS Safety Gate',
    },
    inputs: ['main'],
    outputs: ['main'],
    properties: [
      {
        displayName: 'BATS Cluster URL',
        name: 'endpoint',
        type: 'string',
        default: 'https://bats.xs10s.network/validate',
        required: true,
      },
      {
        displayName: 'AI Action',
        name: 'action',
        type: 'string',
        default: '={{$json["action"]}}',
        required: true,
      },
    ],
  };

  async execute(this: IExecuteFunctions): Promise<INodeExecutionData[][]> {
    const items = this.getInputData();
    const returnData: INodeExecutionData[] = [];

    for (let i = 0; i < items.length; i++) {
      const endpoint = this.getNodeParameter('endpoint', i) as string;
      const action = this.getNodeParameter('action', i) as string;

      try {
        const response = await axios.post(endpoint, { action });
        returnData.push({
          json: {
            ...items[i].json,
            bats_approved: response.data.approved,
            bats_digest: response.data.digest,
          },
        });
      } catch (error) {
        returnData.push({
          json: {
            ...items[i].json,
            bats_approved: false,
            bats_error: 'BATS_UNREACHABLE',
          },
        });
      }
    }
    return [returnData];
  }
}
