package com.example.dynoconf;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.scheduling.annotation.EnableScheduling;

/**
 * Reference Spring Boot consumer for dynoconf. Run it with:
 *
 * <pre>
 * CONFIG_SERVICE_ADDR=localhost:9090 \
 * CONFIG_SERVICE_KEY=payments-api \
 * WATCH_KEY=GREETING \
 * GREETING="env default" \
 * mvn spring-boot:run
 * </pre>
 */
@SpringBootApplication
@EnableScheduling
public class Application {
    public static void main(String[] args) {
        SpringApplication.run(Application.class, args);
    }
}
