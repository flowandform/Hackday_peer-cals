import { SocketEvent } from './SocketEvent'
import { baseUrl, callId, userId } from './window'
import { SocketClient, TypedEmitter } from './ws'
export type ClientSocket = TypedEmitter<SocketEvent>
console.log(baseUrl, callId, userId )
const wsUrl = location.origin.replace(/^http/, 'ws') +
  baseUrl + '/ws/' + callId + '/' + userId

export default new SocketClient<SocketEvent>(wsUrl)
