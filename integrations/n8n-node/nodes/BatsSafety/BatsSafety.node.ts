import {
  IExecuteFunctions,
  INodeExecutionData,
  INodeType,
  INodeTypeDescription,
} from 'n8n-workflow';

import axios from 'axios';

export class WandSafety implements INodeType {
  description: INodeTypeDescription = {
    displayName: 'WAND Safety Gate',
    name: 'wandSafety',
    icon: 'file:wand.svg',
    group: ['transform'],
    version: 1,
    description: 'WAND Deterministic Safety Gate for AI agents',
    defaults: {
      name: 'WAND Safety Gate',
    },
    inputs: ['main'],
    outputs: ['main'],
    properties: [
      {
        displayName: 'WAND Node URL',
        name: 'endpoint',
        type: 'string',
        default: 'https://wand.xs10s.network/validate',
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
            wand_approved: response.data.approved,
            wand_digest: response.data.digest,
          },
        });
      } catch (error) {
        returnData.push({
          json: {
            ...items[i].json,
            wand_approved: false,
            wand_error: 'WAND_UNREACHABLE',
          },
        });
      }
    }
    return [returnData];
  }
}
