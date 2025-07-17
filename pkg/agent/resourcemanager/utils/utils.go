package utils

// todo: read from configmap or what?
func CalculateCPUWeightFromQoSLevel(qosLevel int64) uint64 {
	switch qosLevel {
	case 2:
		return 1000
	case 1:
		return 500
	case 0:
		return 100
	case -1:
		return 50
	default:
		return 100
	}
}

func CalculateCPUQuotaFromQoSLevel(qosLevel int64) uint64 {
	switch qosLevel {
	case 2:
		return 0
	case 1:
		return 0
	case 0:
		return 0
	case -1:
		return 50000
	default:
		return 0
	}
}
