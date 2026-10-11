#import <Foundation/Foundation.h>
#import <ServiceManagement/ServiceManagement.h>
#include <stdlib.h>
#include <string.h>

int ideploy_login_status(void) {
    @autoreleasepool {
        return (int)[SMAppService mainAppService].status;
    }
}

int ideploy_set_login(int enabled, char **error_message) {
    @autoreleasepool {
        NSError *error = nil;
        SMAppService *service = [SMAppService mainAppService];
        BOOL success = enabled ? [service registerAndReturnError:&error]
                               : [service unregisterAndReturnError:&error];
        if (!success && error_message != NULL) {
            NSString *description = error.localizedDescription ?: @"无法更新 macOS 登录项";
            *error_message = strdup(description.UTF8String);
        }
        return success ? 1 : 0;
    }
}
