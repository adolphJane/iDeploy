import Foundation

enum DeploymentEnvironmentLabel {
    static let values = ["default", "test", "production"]

    static func name(_ value: String) -> String {
        switch value {
        case "test": return "测试环境"
        case "production": return "线上环境"
        default: return "默认环境"
        }
    }
}

extension J {
    var deploymentEnvironment: String {
        let value = self["environment"].string
        return value.isEmpty ? "default" : value
    }

    var deploymentEnvironmentLabel: String { DeploymentEnvironmentLabel.name(deploymentEnvironment) }

    var deploymentDisplayName: String {
        let project = self["projectName"].string
        return deploymentEnvironment == "default" ? project : project + " · " + deploymentEnvironmentLabel
    }

    var deploymentLocation: String {
        [self["serverName"].string, self["workloadName"].string].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    var deploymentPublishTitle: String {
        deploymentEnvironment == "default" ? "发布新版本" : "发布到" + deploymentEnvironmentLabel
    }
}
